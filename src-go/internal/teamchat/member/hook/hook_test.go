package hook

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	digest = "0123456789abcdef"
	runID  = "00112233445566ff"
	boot0  = time.Hour
	socket = "/run/user/1000/cc-socks/4242.sock"
	token  = "s3cr3t-inbox-token"
	done0  = `{"done":{"exit":0}}`
)

// world is a member's run directory, a fake clock, the hook's streams and a dial counter.
type world struct {
	t      *testing.T
	env    map[string]string
	run    paths.Run
	root   *homefs.Root
	clk    *clock.Fake
	stdin  string
	stdout bytes.Buffer
	stderr bytes.Buffer
	broken bool
	dials  atomic.Int64
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
	return &world{
		t: t, run: run, root: root, clk: clock.NewFake(boot0, 1_770_000_000_000), stdin: `{}`,
		env: map[string]string{
			paths.EnvRuntime: run.Dir(), "HOME": home, "XDG_RUNTIME_DIR": xdg,
			EnvInboxSocket: socket, EnvInboxToken: token,
		},
	}
}

func (w *world) Write(p []byte) (int, error) {
	if w.broken {
		return 0, errors.New("stdout closed")
	}
	return w.stdout.Write(p)
}

func (w *world) deps() deps {
	return deps{clock: w.clk, dial: func(path string) (*net.UnixConn, error) {
		w.dials.Add(1)
		return dialUnix(path)
	}}
}

func (w *world) env0() cli.Env {
	return cli.Env{
		Stdin: strings.NewReader(w.stdin), Stdout: w, Stderr: &w.stderr, Digest: digest,
		Getenv: func(k string) string { return w.env[k] },
	}
}

// hook runs `teamchat hook <event>` as the CLI would, and checks the exit is 0, stderr empty
// and stdout one line.
func (w *world) hook(event string) string {
	w.t.Helper()
	code := w.start(event)()
	return w.answer(code)
}

// start runs the hook on its own goroutine; the returned func waits for its exit.
func (w *world) start(event string) func() term.ExitCode {
	w.t.Helper()
	w.stdout.Reset()
	w.stderr.Reset()
	inv, err := cli.Parse([]string{"hook", event})
	if err != nil {
		w.t.Fatal(err)
	}
	e, d := w.env0(), w.deps()
	exit := make(chan term.ExitCode, 1)
	go func() { exit <- run(e, inv, d) }()
	return func() term.ExitCode { return <-exit }
}

func (w *world) answer(code term.ExitCode) string {
	w.t.Helper()
	if code != term.ExitOK || w.stderr.Len() != 0 {
		w.t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	out := w.stdout.String()
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		w.t.Fatalf("stdout %q", out)
	}
	return strings.TrimSuffix(out, "\n")
}

// keeper holds keeper.lock as a live keeper does.
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

func (w *world) keeperJSON(link, kind string, nextMS *int64) {
	w.t.Helper()
	id, err := paths.ParseRunID(runID)
	if err != nil {
		w.t.Fatal(err)
	}
	k := state.Keeper{V: state.Version, Run: id, Link: link, NextMS: nextMS, Kind: kind, Desired: state.DesiredApplied}
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

func (w *world) listing() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.run.Dir())
	if err != nil {
		w.t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// link is a test listener on link.sock: each connection's request line is passed on, then
// it is answered with lines; a hanging link holds the connection open with no answer.
type link struct {
	ln   *net.UnixListener
	reqs chan []byte
}

func (w *world) listen(lines ...string) *link {
	w.t.Helper()
	return w.serve(false, lines)
}

func (w *world) hang() *link {
	w.t.Helper()
	return w.serve(true, nil)
}

func (w *world) serve(hang bool, lines []string) *link {
	w.t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: w.run.LinkSock(), Net: "unix"})
	if err != nil {
		w.t.Fatal(err)
	}
	l := &link{ln: ln, reqs: make(chan []byte, 8)}
	w.t.Cleanup(func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			w.t.Error(err)
		}
	})
	go func() {
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			l.answer(c, hang, lines)
		}
	}()
	return l
}

func (l *link) answer(c *net.UnixConn, hang bool, lines []string) {
	defer func() {
		if err := c.Close(); err != nil {
			return
		}
	}()
	r := bufio.NewReader(c)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	l.reqs <- line[:len(line)-1]
	if hang {
		if _, err := r.ReadByte(); err != nil {
			return
		}
	}
	for _, s := range lines {
		if _, err := c.Write([]byte(s + "\n")); err != nil {
			return
		}
	}
}

// sent is one request the link read, decoded, and its hook args.
type sent struct {
	req  wire.Request
	args wire.HookArgs
}

func (l *link) request(t *testing.T) sent {
	t.Helper()
	select {
	case line := <-l.reqs:
		req, err := wire.DecodeRequest(line)
		if err != nil {
			t.Fatal(err)
		}
		args, err := wire.DecodeHookArgs(req.Op, req.Args)
		if err != nil {
			t.Fatalf("%v: %v", req, err)
		}
		return sent{req: req, args: args}
	case <-time.After(10 * time.Second):
		t.Fatal("no request")
	}
	return sent{}
}

// Every kit hook has its op, and the parser's events are the kit's.
func TestEveryKitHookHasAnOp(t *testing.T) {
	args := make([]string, 0, len(kit.Hooks))
	for _, h := range kit.Hooks {
		ev, ok := eventOf(h.Arg)
		if !ok || ev.name != h.Event {
			t.Fatalf("%+v: %+v", h, ev)
		}
		args = append(args, h.Arg)
	}
	if !slices.Equal(args, cli.HookEvents) {
		t.Fatalf("kit %v, cli %v", args, cli.HookEvents)
	}
	want := map[string]string{
		"session-start": wire.OpHookSessionStart, "user-prompt-submit": wire.OpHookPrompt,
		"stop": wire.OpHookStop, "session-end": wire.OpHookSessionEnd,
	}
	for arg, op := range want {
		if ev, ok := eventOf(arg); !ok || ev.op != op {
			t.Fatalf("%s: %+v", arg, ev)
		}
	}
}

// n3e-D15: an event outside kit.Hooks is 64, the only exit but 0.
func TestABadEventIs64(t *testing.T) {
	w := newWorld(t)
	for _, inv := range []cli.Invocation{{Args: []string{"pre-tool-use"}}, {}} {
		w.stderr.Reset()
		if code := run(w.env0(), inv, w.deps()); code != exits.ExitUsage || w.stderr.Len() == 0 {
			t.Fatalf("%v: exit %d, stderr %q", inv.Args, code, w.stderr.String())
		}
	}
	if w.dials.Load() != 0 || w.stdout.Len() != 0 {
		t.Fatalf("%d dials, stdout %q", w.dials.Load(), w.stdout.String())
	}
	if _, err := cli.Parse([]string{"hook", "pre-tool-use"}); err == nil {
		t.Fatal("the parser took an event outside kit.Hooks")
	}
}

// B 2.8: no TEAMCHAT_RUNTIME, or one that names no session, is {} at once, with no dial.
func TestNoRuntime(t *testing.T) {
	for _, dir := range []string{"", "/tmp/elsewhere/" + runID} {
		w := newWorld(t)
		w.env[paths.EnvRuntime] = dir
		w.stdin = "\xff not even json"
		for _, h := range kit.Hooks {
			if got := w.hook(h.Arg); got != "{}" {
				t.Fatalf("%q %s: %s", dir, h.Arg, got)
			}
		}
		if w.dials.Load() != 0 {
			t.Fatalf("%q: %d dials", dir, w.dials.Load())
		}
	}
}

// A link that answers: its line is printed byte for byte, whatever its shape.
func TestALinkThatAnswers(t *testing.T) {
	for _, line := range []string{
		`{}`,
		string(hooktext.Block(hooktext.StopPending(hooktext.Counts{Total: 2, Humans: 2})).Line),
		`{"systemMessage":"x"}`,
		` {"a" : [1, 2]} `,
	} {
		w := newWorld(t)
		l := w.listen(line, done0)
		if got := w.hook("stop"); got != line {
			t.Fatalf("%s, want %s", got, line)
		}
		req := l.request(t).req
		if req.Op != wire.OpHookStop || req.Kit != digest || req.DeadlineMS != (boot0+300*time.Millisecond).Milliseconds() {
			t.Fatalf("request %v", req)
		}
	}
}

// OF1: one team stopped beside a running one is one answer, printed whole, exit 0; the hook
// never blocks on either.
func TestAStoppedTeamBesideARunningOne(t *testing.T) {
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := paths.ParseTeamID("beta")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join([]string{hooktext.TeamFailed(alpha, state.FailedForbidden), hooktext.TeamDown(beta), hooktext.Pending(hooktext.Counts{Total: 3, Agents: 3})}, "\n")
	line := string(hooktext.Context("UserPromptSubmit", text).Line)
	w := newWorld(t)
	w.listen(line, done0)
	if got := w.hook("user-prompt-submit"); got != line {
		t.Fatalf("%s", got)
	}
}

// The answers that are not one: the hook answers from its own template.
func TestALinkThatAnswersBadly(t *testing.T) {
	for name, lines := range map[string][]string{
		"close before any line":     nil,
		"close before done":         {`{}`},
		"not an object":             {`[]`, done0},
		"not json":                  {`{`, done0},
		"a done with no answer":     {done0},
		"a malformed done":          {`{}`, `{"done":{"exit":"0"}}`},
		"a failing done":            {`{}`, `{"done":{"exit":69,"error":{"exit":69,"code":"no-link","team":null,"message":"x"}}}`},
		"an ack":                    {`{}`, `{"done":{"exit":0,"ack":"0a1b2c3d"}}`},
		"a line past the bound":     {`{"a":"` + strings.Repeat("a", maxAnswer) + `"}`, done0},
		"a malformed refusing done": {`{"done":{"exit":69}}`},
	} {
		w := newWorld(t)
		w.keeper()
		w.keeperJSON(state.LinkRunning, state.KindHost, nil)
		w.listen(lines...)
		if got := w.hook("session-start"); !strings.Contains(got, "teamchat is starting or restarting") {
			t.Fatalf("%s: %s", name, got)
		}
	}
}

// A link that refuses the request (its done's error) is told as that error's message on
// SessionStart and UserPromptSubmit, and nothing on Stop.
func TestALinkThatRefuses(t *testing.T) {
	done := wire.Done{Error: exits.New(exits.CodeBundle, nil, "the session runs another teamchat kit; end this session and launch it again")}
	w := newWorld(t)
	w.listen(string(done.Encode()))
	want := string(hooktext.Context("UserPromptSubmit", done.Error.Message).Line)
	if got := w.hook("user-prompt-submit"); got != want {
		t.Fatalf("%s, want %s", got, want)
	}
	if got := w.hook("stop"); got != "{}" {
		t.Fatalf("stop: %s", got)
	}
}

// B 2.8: a link that does not answer within 300 ms is answered from the hook's own template,
// chosen by keeper.json and the keeper.lock probe: the recovery on SessionStart and
// UserPromptSubmit, {} on Stop and SessionEnd; exit 0, and no file written.
func TestALinkThatNeverAnswers(t *testing.T) {
	next := (boot0 + 4200*time.Millisecond).Milliseconds()
	for _, tc := range []struct {
		name   string
		keeper bool
		link   string
		kind   string
		next   *int64
		want   string
	}{
		{"restarting", true, state.LinkRestarting, state.KindHost, &next, "teamchat is restarting (next try in 4 s)"},
		{"starting", true, state.LinkRunning, state.KindCCY, nil, "teamchat is starting or restarting"},
		{"failed ccy", true, state.LinkFailed, state.KindCCY, nil, "ask the owner to run `ccy-teams restart work-1`"},
		{"failed lxc", true, state.LinkFailed, state.KindLXC, nil, "ask the owner to run `ccy-teams restart work-1`"},
		{"failed host", true, state.LinkFailed, state.KindHost, nil, "end this session and launch it again"},
		{"keeper.json missing", true, "", "", nil, "teamchat is starting or restarting"},
		{"keeper gone", false, "", "", nil, "end this session and launch it again"},
	} {
		w := newWorld(t)
		if tc.keeper {
			w.keeper()
		}
		if tc.link != "" {
			w.keeperJSON(tc.link, tc.kind, tc.next)
		}
		l := w.hang()
		before := w.listing()
		for _, h := range kit.Hooks {
			got := w.hungHook(l, h.Arg)
			want := tc.want
			if h.Arg == "stop" || h.Arg == "session-end" {
				want = "{}"
			} else if !strings.HasPrefix(got, `{"hookSpecificOutput":{"hookEventName":"`+h.Event+`","additionalContext":"`) {
				t.Fatalf("%s %s: %s", tc.name, h.Arg, got)
			}
			if !strings.Contains(got, want) {
				t.Fatalf("%s %s: %s", tc.name, h.Arg, got)
			}
		}
		if after := w.listing(); !slices.Equal(before, after) {
			t.Fatalf("%s: the run directory changed: %v to %v", tc.name, before, after)
		}
	}
}

// hungHook runs the hook against a link that never answers, to its deadline on a fresh clock.
func (w *world) hungHook(l *link, event string) string {
	w.t.Helper()
	w.clk = clock.NewFake(boot0, 1_770_000_000_000)
	wait := w.start(event)
	l.request(w.t)
	if err := w.clk.AwaitDue(w.t.Context(), 300*time.Millisecond); err != nil {
		w.t.Fatal(err)
	}
	w.clk.Advance(300 * time.Millisecond)
	return w.answer(wait())
}

// No link.sock at all is answered at once, without the deadline.
func TestNoLinkSocket(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindCCY, new(boot0.Milliseconds()))
	got := w.hook("user-prompt-submit")
	want := string(hooktext.Context("UserPromptSubmit", client.ReadDown(w.run).Error(nil, boot0).Message).Line)
	if got != want || w.dials.Load() != 1 || w.clk.Boot() != boot0 {
		t.Fatalf("%s, want %s; %d dials at %v", got, want, w.dials.Load(), w.clk.Boot())
	}
	if got := w.hook("session-end"); got != "{}" {
		t.Fatalf("session-end: %s", got)
	}
}

// HK1: the payload's bound, its UTF-8 and its one object; each request still carries the
// inbox variables.
func TestThePayload(t *testing.T) {
	pad := func(n int) string {
		head := `{"session_id":"abc","pad":"`
		return head + strings.Repeat("a", n-len(head)-2) + `"}`
	}
	for _, tc := range []struct {
		stdin string
		want  string
	}{
		{pad(maxStdin), ""},
		{pad(maxStdin + 1), wire.UnusableSize},
		{"{\"a\":\"\xff\"}", wire.UnusableUTF8},
		{`[]`, wire.UnusableJSON},
		{`"x"`, wire.UnusableJSON},
		{`{}{}`, wire.UnusableJSON},
		{`{} junk`, wire.UnusableJSON},
		{``, wire.UnusableJSON},
		{`{"n":1.5}`, wire.UnusableJSON},
	} {
		w := newWorld(t)
		w.stdin = tc.stdin
		l := w.listen(`{}`, done0)
		w.hook("user-prompt-submit")
		args := l.request(t).args
		if tc.want == "" {
			if args.Payload != wire.PayloadOK || args.Unusable != nil || args.SessionID == nil || *args.SessionID != "abc" {
				t.Fatalf("%d bytes: %v", len(tc.stdin), args)
			}
		} else if args.Payload != wire.PayloadUnusable || args.Unusable == nil || *args.Unusable != tc.want || args.SessionID != nil {
			t.Fatalf("%.40q: %v", tc.stdin, args)
		}
		if args.Socket == nil || *args.Socket != socket || args.Token == nil || *args.Token != token {
			t.Fatalf("%.40q: the inbox variables are lost: %v", tc.stdin, args)
		}
	}
}

// A stdin that fails to read is unusable json: nothing whole was read.
func TestAStdinThatFails(t *testing.T) {
	if got := payload(failingReader{}, wire.OpHookPrompt); got.Payload != wire.PayloadUnusable || *got.Unusable != wire.UnusableJSON {
		t.Fatalf("%v", got)
	}
	if got := payload(nil, wire.OpHookPrompt); got.Payload != wire.PayloadUnusable || *got.Unusable != wire.UnusableJSON {
		t.Fatalf("no stdin: %v", got)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("EIO") }

// The fields each event uses, each absent when it is not a string within its bound (or, for
// stop, the literal false).
func TestTheFields(t *testing.T) {
	long := strings.Repeat("a", wire.MaxHookTranscript+1)
	for _, tc := range []struct {
		event, stdin string
		check        func(wire.HookArgs) bool
	}{
		{"session-start", `{"session_id":"s","transcript_path":"/t.jsonl","source":"resume","reason":"exit","stop_hook_active":false}`, func(a wire.HookArgs) bool {
			return *a.SessionID == "s" && *a.TranscriptPath == "/t.jsonl" && *a.Source == "resume" && a.Reason == nil && !a.StopHookFalse
		}},
		{"user-prompt-submit", `{"session_id":7,"transcript_path":"` + long + `","source":"resume"}`, func(a wire.HookArgs) bool {
			return a.SessionID == nil && a.TranscriptPath == nil && a.Source == nil
		}},
		{"stop", `{"stop_hook_active":false}`, func(a wire.HookArgs) bool { return a.StopHookFalse }},
		{"stop", `{"stop_hook_active":true}`, func(a wire.HookArgs) bool { return !a.StopHookFalse }},
		{"stop", `{"stop_hook_active":"false"}`, func(a wire.HookArgs) bool { return !a.StopHookFalse }},
		{"stop", `{}`, func(a wire.HookArgs) bool { return !a.StopHookFalse }},
		{"session-end", `{"reason":"logout","source":"x"}`, func(a wire.HookArgs) bool { return *a.Reason == "logout" && a.Source == nil }},
	} {
		w := newWorld(t)
		w.stdin = tc.stdin
		l := w.listen(`{}`, done0)
		w.hook(tc.event)
		if args := l.request(t).args; !tc.check(args) {
			t.Fatalf("%s %s: %v", tc.event, tc.stdin, args)
		}
	}
	// the inbox variables: unset, too long, or not UTF-8, are absent.
	for _, env := range []map[string]string{
		{EnvInboxSocket: strings.Repeat("s", wire.MaxHookSocket+1)},
		{EnvInboxSocket: "/s\xff", EnvInboxToken: ""},
	} {
		w := newWorld(t)
		delete(w.env, EnvInboxToken)
		maps.Copy(w.env, env)
		l := w.listen(`{}`, done0)
		w.hook("stop")
		if args := l.request(t).args; args.Socket != nil || args.Token != nil {
			t.Fatalf("%v", args)
		}
	}
}

// B11: the token is in the request the link reads, and never in stderr or a formatted request.
func TestTheTokenStaysSecret(t *testing.T) {
	w := newWorld(t)
	l := w.listen(`{}`, done0)
	w.hook("session-start")
	got := l.request(t)
	if *got.args.Token != token {
		t.Fatalf("token %v", got.args)
	}
	for _, text := range []string{fmt.Sprintf("%v %+v %#v %s", got.req, got.req, got.req, got.args), w.stderr.String(), w.stdout.String()} {
		if strings.Contains(text, token) {
			t.Fatalf("the token shows: %s", text)
		}
	}
}

// A failed stdout write keeps exit 0.
func TestAFailedStdout(t *testing.T) {
	w := newWorld(t)
	w.broken = true
	w.listen(`{}`, done0)
	if code := w.start("stop")(); code != term.ExitOK || w.stderr.Len() != 0 {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
}

// A request that cannot be framed (a binary built with no digest) is {}: nothing true to say.
func TestNoDigest(t *testing.T) {
	w := newWorld(t)
	inv, err := cli.Parse([]string{"hook", "session-start"})
	if err != nil {
		t.Fatal(err)
	}
	e := w.env0()
	e.Digest = ""
	if code := run(e, inv, w.deps()); code != term.ExitOK || w.stdout.String() != "{}\n" || w.dials.Load() != 0 {
		t.Fatalf("exit %d, stdout %q, %d dials", code, w.stdout.String(), w.dials.Load())
	}
}

// Main is the cli.RunFunc cmd/teamchat registers; with no runtime it answers at once.
func TestMainIsTheVerb(t *testing.T) {
	w := newWorld(t)
	inv, err := cli.Parse([]string{"hook", "session-end"})
	if err != nil {
		t.Fatal(err)
	}
	var f cli.RunFunc = Main
	e := w.env0()
	e.Getenv = func(string) string { return "" }
	if code := f(e, inv); code != term.ExitOK || w.stdout.String() != "{}\n" {
		t.Fatalf("exit %d, stdout %q", code, w.stdout.String())
	}
}
