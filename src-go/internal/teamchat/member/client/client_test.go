package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	kit   = "0123456789abcdef"
	runID = "00112233445566ff"
	boot0 = time.Hour
)

// world is a member's run directory with no link yet, a fake clock and the CLI's streams.
type world struct {
	t      *testing.T
	env    map[string]string
	run    paths.Run
	root   *homefs.Root
	clk    *clock.Fake
	stdout bytes.Buffer
	stderr bytes.Buffer
	out    stdoutWriter
}

// stdoutWriter is the CLI's stdout: the buffer, or a failure.
type stdoutWriter struct {
	w   *bytes.Buffer
	err error
}

func (s *stdoutWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.w.Write(p)
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	home, xdg := filepath.Join(base, "h"), filepath.Join(base, "x")
	layout, err := paths.HomeLayout(home, xdg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseRunID(runID)
	if err != nil {
		t.Fatal(err)
	}
	run := layout.Run(id)
	if err = os.MkdirAll(run.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := homefs.Open(run.Dir(), homefs.Private)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	w := &world{
		t: t, run: run, root: root, clk: clock.NewFake(boot0, 1_770_000_000_000),
		env: map[string]string{paths.EnvRuntime: run.Dir(), "HOME": home, "XDG_RUNTIME_DIR": xdg},
	}
	w.out.w = &w.stdout
	return w
}

func (w *world) cliEnv() cli.Env {
	return cli.Env{Stdout: &w.out, Stderr: &w.stderr, Digest: kit, Getenv: func(k string) string { return w.env[k] }}
}

// do runs the verb of args as the CLI would.
func (w *world) do(args ...string) term.ExitCode {
	w.t.Helper()
	inv, err := cli.Parse(args)
	if err != nil {
		w.t.Fatalf("parse %v: %v", args, err)
	}
	return run(w.cliEnv(), inv, inv.Verb.Name, deps{clock: w.clk, dial: dialUnix})
}

// async runs do on its own goroutine; the exit arrives on the channel.
func (w *world) async(args ...string) <-chan term.ExitCode {
	w.t.Helper()
	inv, err := cli.Parse(args)
	if err != nil {
		w.t.Fatalf("parse %v: %v", args, err)
	}
	e := w.cliEnv()
	out := make(chan term.ExitCode, 1)
	go func() { out <- run(e, inv, inv.Verb.Name, deps{clock: w.clk, dial: dialUnix}) }()
	return out
}

// errorObject is the last stderr line, decoded.
func (w *world) errorObject() exits.Error {
	w.t.Helper()
	lines := strings.Split(strings.TrimSpace(w.stderr.String()), "\n")
	e, err := exits.Decode([]byte(lines[len(lines)-1]))
	if err != nil {
		w.t.Fatalf("stderr %q: %v", w.stderr.String(), err)
	}
	return e
}

// keeper holds keeper.lock as a live keeper does, released at the test's end.
func (w *world) keeper() {
	w.t.Helper()
	k, err := lock.TakeKeeper(w.t.Context(), lock.SystemClock(), w.root, w.run)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := k.Release(); err != nil && !errors.Is(err, lock.ErrReleased) {
			w.t.Error(err)
		}
	})
}

// keeperJSON writes keeper.json with the link's state.
func (w *world) keeperJSON(link string, kind string, nextMS *int64) {
	w.t.Helper()
	k := state.Keeper{V: state.Version, Run: w.runID(), Link: link, NextMS: nextMS, Kind: kind, Desired: state.DesiredApplied}
	if link == state.LinkFailed {
		k.Why = new(state.WhyCrashLoop)
	}
	if kind == state.KindCCY || kind == state.KindLXC {
		s, err := paths.ParseSessionName("work-1")
		if err != nil {
			w.t.Fatal(err)
		}
		k.Session = &s
	}
	if err := state.Write(w.root, w.run.KeeperJSON(), k); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) runID() paths.RunID {
	w.t.Helper()
	id, err := paths.ParseRunID(runID)
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// link is a test listener on link.sock speaking wire.
type link struct {
	ln   *net.UnixListener
	reqs chan wire.Request
	acks chan string
}

// listen serves link.sock: each connection's request is decoded and passed on, answered with
// lines, then any ack read is passed on; close is a close before done.
func (w *world) listen(lines ...string) *link {
	w.t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: w.run.LinkSock(), Net: "unix"})
	if err != nil {
		w.t.Fatal(err)
	}
	l := &link{ln: ln, reqs: make(chan wire.Request, 8), acks: make(chan string, 8)}
	w.t.Cleanup(func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			w.t.Error(err)
		}
	})
	go l.serve(lines)
	return l
}

func (l *link) serve(lines []string) {
	for {
		c, err := l.ln.AcceptUnix()
		if err != nil {
			return
		}
		l.answer(c, lines)
	}
}

func (l *link) answer(c *net.UnixConn, lines []string) {
	defer func() {
		if err := c.Close(); err != nil {
			panic(err)
		}
	}()
	r := bufio.NewReader(c)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	req, err := wire.DecodeRequest(line[:len(line)-1])
	if err != nil {
		panic(err)
	}
	l.reqs <- req
	done := false
	for _, s := range lines {
		if _, werr := c.Write([]byte(s + "\n")); werr != nil {
			return
		}
		done = done || strings.HasPrefix(s, `{"done":`)
	}
	if !done {
		return
	}
	ack, err := r.ReadBytes('\n')
	if err != nil {
		l.acks <- ""
		return
	}
	a, err := wire.DecodeAck(ack[:len(ack)-1])
	if err != nil {
		panic(err)
	}
	l.acks <- a.Ack
}

func (l *link) request(t *testing.T) wire.Request {
	t.Helper()
	select {
	case r := <-l.reqs:
		return r
	case <-t.Context().Done():
		t.Fatal("no request")
	}
	return wire.Request{}
}

func (l *link) ack(t *testing.T) string {
	t.Helper()
	select {
	case a := <-l.acks:
		return a
	case <-t.Context().Done():
		t.Fatal("no ack read")
	}
	return ""
}

const (
	record1 = `{"v":1,"kind":"more","team":"alpha","pending":1}`
	record2 = `{"v":1,"kind":"more","team":"beta","pending":2}`
	doneAck = `{"done":{"exit":0,"ack":"0a1b2c3d"}}`
	doneOK  = `{"done":{"exit":0}}`
)

var eventID = "$" + strings.Repeat("a", 43)

func args(t *testing.T, r wire.Request) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Args, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// B 2.4.4: one request with deadline_ms from the boot clock and the CLI's own digest; the records
// copied to stdout, flushed, then the ack sent back.
func TestRecvCopiesThenAcks(t *testing.T) {
	w := newWorld(t)
	l := w.listen(record1, record2, doneAck)
	if code := w.do("recv"); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	req := l.request(t)
	if req.Op != wire.OpRecv || req.Kit != kit || req.DeadlineMS != (boot0+2*time.Second).Milliseconds() || req.Stdin != nil {
		t.Fatalf("request %+v", req)
	}
	if a := args(t, req); a["team"] != nil || len(a) != 1 {
		t.Fatalf("args %v", a)
	}
	if got := w.stdout.String(); got != record1+"\n"+record2+"\n" {
		t.Fatalf("stdout %q", got)
	}
	if a := l.ack(t); a != "0a1b2c3d" {
		t.Fatalf("ack %q", a)
	}
	if w.stderr.Len() != 0 {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// A failed stdout sends no ack, so the link returns the items.
func TestAFailedStdoutSendsNoAck(t *testing.T) {
	w := newWorld(t)
	w.out.err = errors.New("closed")
	l := w.listen(record1, doneAck)
	if code := w.do("recv", "--team", "alpha"); code != exits.ExitInternal {
		t.Fatalf("exit %d", code)
	}
	if a := args(t, l.request(t)); a["team"] != "alpha" {
		t.Fatalf("args %v", a)
	}
	if a := l.ack(t); a != "" {
		t.Fatalf("acked %q", a)
	}
}

// The done's error object goes to stderr and is the exit; inbox and status send no ack.
func TestAFailureIsCopied(t *testing.T) {
	w := newWorld(t)
	failure := exits.New(exits.CodeNotFound, nil, "no event {}", exits.Name("$x"))
	line := wire.Done{Error: failure}.Encode()
	l := w.listen(string(line))
	if code := w.do("status"); code != exits.ExitNotFound {
		t.Fatalf("exit %d", code)
	}
	if e := w.errorObject(); e.Code != exits.CodeNotFound {
		t.Fatalf("error %+v", e)
	}
	if req := l.request(t); req.Op != wire.OpStatus {
		t.Fatalf("op %s", req.Op)
	}
	if a := l.ack(t); a != "" {
		t.Fatalf("acked %q", a)
	}
}

// Each verb's args, with wait's and history's defaults (n3c-D7, n3c-D8).
func TestTheVerbsArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		op   string
		want string
	}{
		{[]string{"inbox"}, wire.OpInbox, `{"team":null}`},
		{[]string{"verify", eventID}, wire.OpVerify, `{"id":"$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","team":null}`},
		{[]string{"verify", eventID, "--team", "alpha"}, wire.OpVerify, `{"id":"$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","team":"alpha"}`},
		{[]string{"history"}, wire.OpHistory, `{"limit":20,"room":"main","team":null}`},
		{[]string{"history", "--room", "questions", "--limit", "5"}, wire.OpHistory, `{"limit":5,"room":"questions","team":null}`},
		{[]string{"wait"}, wire.OpWait, `{"team":null,"timeout_s":110}`},
		{[]string{"wait", "--timeout", "7", "--team", "beta"}, wire.OpWait, `{"team":"beta","timeout_s":7}`},
	} {
		w := newWorld(t)
		l := w.listen(doneOK)
		if code := w.do(tc.args...); code != term.ExitOK {
			t.Fatalf("%v: exit %d %q", tc.args, code, w.stderr.String())
		}
		req := l.request(t)
		got, err := json.Marshal(args(t, req))
		if err != nil {
			t.Fatal(err)
		}
		if req.Op != tc.op || string(got) != tc.want {
			t.Fatalf("%v: %s %s", tc.args, req.Op, got)
		}
	}
}

// wait's deadline is its timeout; one past 3600 s, or 0, is usage.
func TestWaitTimeoutBounds(t *testing.T) {
	w := newWorld(t)
	l := w.listen(doneOK)
	if code := w.do("wait", "--timeout", "30"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if req := l.request(t); req.DeadlineMS != (boot0 + 30*time.Second).Milliseconds() {
		t.Fatalf("deadline %d", req.DeadlineMS)
	}
	for _, s := range []string{"3601", "0"} {
		w.stderr.Reset()
		if code := w.do("wait", "--timeout", s); code != exits.ExitUsage {
			t.Fatalf("--timeout %s: exit %d", s, code)
		}
		if e := w.errorObject(); e.Code != exits.CodeUsage {
			t.Fatalf("error %+v", e)
		}
	}
	w.stderr.Reset()
	if code := w.do("history", "--limit", "101"); code != exits.ExitUsage {
		t.Fatalf("history --limit 101: exit %d", code)
	}
}

// No TEAMCHAT_RUNTIME is 69 no-session; one that is no run directory is too.
func TestNoSession(t *testing.T) {
	w := newWorld(t)
	for _, dir := range []string{"", "/tmp", "relative/" + runID} {
		w.env[paths.EnvRuntime] = dir
		w.stderr.Reset()
		if code := w.do("recv"); code != exits.ExitNoRuntime {
			t.Fatalf("%q: exit %d", dir, code)
		}
		if e := w.errorObject(); e.Code != exits.CodeNoSession {
			t.Fatalf("%q: error %+v", dir, e)
		}
	}
}

// A CLI built without its digest cannot frame a request.
func TestNoDigest(t *testing.T) {
	w := newWorld(t)
	w.listen(doneOK)
	inv, err := cli.Parse([]string{"recv"})
	if err != nil {
		t.Fatal(err)
	}
	e := w.cliEnv()
	e.Digest = ""
	if code := run(e, inv, "recv", deps{clock: w.clk, dial: dialUnix}); code != exits.ExitInternal {
		t.Fatalf("exit %d", code)
	}
}

// Main is the verb as cmd/teamchat registers it, on the system's clock and socket.
func TestMain_(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	inv, err := cli.Parse([]string{"inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if code := Main("inbox")(w.cliEnv(), inv); code != exits.ExitNoRuntime {
		t.Fatalf("exit %d", code)
	}
}
