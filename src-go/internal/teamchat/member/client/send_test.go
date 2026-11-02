package client

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// untouched is a stdin a verb must not read.
type untouched struct{ read bool }

func (u *untouched) Read([]byte) (int, error) {
	u.read = true
	return 0, io.EOF
}

// failing is a stdin whose read fails.
type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("EIO") }

// SD-c1: each send verb's op, args and stdin per flag combination; team null without --team;
// the 30 s deadline on the boot clock and the CLI's own digest.
func TestSDc1RequestBytes(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		stdin *string
		op    string
		want  string
	}{
		{[]string{"say", "--to", "bob,carol"}, new("hello"), wire.OpSay, `{"team":null,"to":["bob","carol"],"to_coordinator":false}`},
		{[]string{"say", "--to-coordinator", "--team", "alpha"}, new("hello"), wire.OpSay, `{"team":"alpha","to":[],"to_coordinator":true}`},
		{
			[]string{"ping", "review", "docs/a.md", "--to", "bob", "--branch", "main"},
			nil, wire.OpPing,
			`{"branch":"main","ref":"docs/a.md","team":null,"to":["bob"],"to_coordinator":false,"verb":"review"}`,
		},
		{
			[]string{"ping", "halt", "--to-coordinator", "--team", "beta"},
			nil, wire.OpPing,
			`{"branch":null,"ref":null,"team":"beta","to":[],"to_coordinator":true,"verb":"halt"}`,
		},
		{
			[]string{"reply", eventID},
			new("hello"), wire.OpReply,
			`{"id":"` + eventID + `","ref":null,"team":null,"to_coordinator":false,"verb":null}`,
		},
		{
			[]string{"reply", eventID, "--to-coordinator", "--team", "alpha"},
			new("hello"), wire.OpReply,
			`{"id":"` + eventID + `","ref":null,"team":"alpha","to_coordinator":true,"verb":null}`,
		},
		{
			[]string{"reply", eventID, "--verb", "done", "--ref", "abc123"},
			nil, wire.OpReply,
			`{"id":"` + eventID + `","ref":"abc123","team":null,"to_coordinator":false,"verb":"done"}`,
		},
	} {
		w := newWorld(t)
		w.stdin = strings.NewReader("hello\n")
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
		if req.Kit != kit || req.DeadlineMS != (boot0+30*time.Second).Milliseconds() {
			t.Fatalf("%v: request %+v", tc.args, req)
		}
		if (req.Stdin == nil) != (tc.stdin == nil) || req.Stdin != nil && *req.Stdin != *tc.stdin {
			t.Fatalf("%v: stdin %v", tc.args, req.Stdin)
		}
	}
}

// SD-c2: the text on stdin, at most 4096 bytes after one trailing newline is removed.
func TestSDc2StdinSent(t *testing.T) {
	full := strings.Repeat("x", 4096)
	for _, tc := range []struct{ in, want string }{
		{full, full},
		{full + "\n", full},
		{"text\n", "text"},
		{"text\n\n", "text\n"},
		{"é ü", "é ü"},
	} {
		w := newWorld(t)
		w.stdin = strings.NewReader(tc.in)
		l := w.listen(doneOK)
		if code := w.do("say", "--to", "bob"); code != term.ExitOK {
			t.Fatalf("%d bytes: exit %d %q", len(tc.in), code, w.stderr.String())
		}
		if req := l.request(t); req.Stdin == nil || *req.Stdin != tc.want {
			t.Fatalf("%d bytes: stdin of %d bytes", len(tc.in), len(deref(req.Stdin)))
		}
	}
}

// deref is a text that may be absent, "" when it is.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SD-c2: over 4096 bytes is 10 size, empty 10 empty, not UTF-8 10 schema; each refused before
// any connection (no link listens, so a connection would have been 69).
func TestSDc2StdinRefused(t *testing.T) {
	for _, tc := range []struct {
		in   string
		exit term.ExitCode
		code string
	}{
		{strings.Repeat("x", 4097), exits.ExitRefused, exits.CodeSize},
		{strings.Repeat("x", 4097) + "\n", exits.ExitRefused, exits.CodeSize},
		{strings.Repeat("x", 9000), exits.ExitRefused, exits.CodeSize},
		{"", exits.ExitRefused, exits.CodeEmpty},
		{"\n", exits.ExitRefused, exits.CodeEmpty},
		{"bad \xff byte", exits.ExitRefused, exits.CodeSchema},
	} {
		for _, verb := range [][]string{{"say", "--to", "bob"}, {"reply", eventID}} {
			w := newWorld(t)
			w.stdin = strings.NewReader(tc.in)
			if code := w.do(verb...); code != tc.exit {
				t.Fatalf("%v %d bytes: exit %d %q", verb, len(tc.in), code, w.stderr.String())
			}
			if e := w.errorObject(); e.Code != tc.code {
				t.Fatalf("%v %d bytes: error %+v", verb, len(tc.in), e)
			}
		}
	}
}

// SD-c2: a terminal on stdin is 64 usage, found before anything is read.
func TestSDc2TerminalIsUsage(t *testing.T) {
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pty.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, verb := range [][]string{{"say", "--to", "bob"}, {"reply", eventID}} {
		w := newWorld(t)
		w.stdin = pty
		if code := w.do(verb...); code != exits.ExitUsage {
			t.Fatalf("%v: exit %d %q", verb, code, w.stderr.String())
		}
		if e := w.errorObject(); e.Code != exits.CodeUsage || !strings.Contains(e.Message, "text on stdin") {
			t.Fatalf("%v: error %+v", verb, e)
		}
	}
}

// SD-c2 (n3d-D15): ping and reply --verb send stdin null and never read it, so an inherited
// stdin cannot block them.
func TestSDc2NoStdinRead(t *testing.T) {
	for _, verb := range [][]string{{"ping", "check", "--to", "bob"}, {"reply", eventID, "--verb", "ack"}} {
		w := newWorld(t)
		in := &untouched{}
		w.stdin = in
		l := w.listen(doneOK)
		if code := w.do(verb...); code != term.ExitOK {
			t.Fatalf("%v: exit %d %q", verb, code, w.stderr.String())
		}
		if req := l.request(t); req.Stdin != nil || in.read {
			t.Fatalf("%v: stdin %v, read %v", verb, req.Stdin, in.read)
		}
	}
}

// A stdin whose read fails is internal, and nothing is sent.
func TestStdinReadFails(t *testing.T) {
	w := newWorld(t)
	w.stdin = failing{}
	if code := w.do("say", "--to", "bob"); code != exits.ExitInternal {
		t.Fatalf("exit %d", code)
	}
	if e := w.errorObject(); e.Code != exits.CodeInternal || !strings.Contains(e.Message, "read its stdin") {
		t.Fatalf("error %+v", e)
	}
}

func recordLine(t *testing.T, r records.Record) string {
	t.Helper()
	line, err := records.Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(line), "\n")
}

// SD-c3: a sent and a queued sent are copied to stdout and exit 0; an error object is copied
// to stderr with its exit.
func TestSDc3Answers(t *testing.T) {
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []records.Sent{
		{Team: alpha, ID: new(eventID), Room: "main"},
		{Team: alpha, Room: "main", Queued: true},
	} {
		w := newWorld(t)
		w.stdin = strings.NewReader("hi")
		line := recordLine(t, r)
		w.listen(line, doneOK)
		if code := w.do("say", "--to", "bob"); code != term.ExitOK {
			t.Fatalf("%+v: exit %d %q", r, code, w.stderr.String())
		}
		if w.stdout.String() != line+"\n" || w.stderr.Len() != 0 {
			t.Fatalf("%+v: stdout %q stderr %q", r, w.stdout.String(), w.stderr.String())
		}
	}
	w := newWorld(t)
	w.listen(string(wire.Done{Error: exits.New(exits.CodeRate, &alpha, "the send budget is spent")}.Encode()))
	if code := w.do("ping", "review", "docs/a.md", "--to", "bob"); code != exits.ExitRate {
		t.Fatalf("exit %d", code)
	}
	if e := w.errorObject(); e.Code != exits.CodeRate || w.stdout.Len() != 0 {
		t.Fatalf("error %+v stdout %q", e, w.stdout.String())
	}
}

// A send never reconnects: with no link it is 69 at once, with nothing waited.
func TestASendNeverRetries(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindCCY, new((boot0 + 4*time.Second).Milliseconds()))
	w.stdin = strings.NewReader("hi")
	if code := w.do("say", "--to", "bob"); code != exits.ExitNoRuntime {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	if w.clk.Boot() != boot0 || w.clk.Waiters() != 0 {
		t.Fatalf("the client waited: boot %v, %d waiters", w.clk.Boot(), w.clk.Waiters())
	}
	if e := w.errorObject(); e.Code != exits.CodeNoLink {
		t.Fatalf("error %+v", e)
	}
}

// B-m3: a send whose request reached the link before it closed may already be charged, so the
// answer says to check status before sending again, not that the messages wait on the server.
func TestASendCutOffAfterItsRequestSaysCheckStatus(t *testing.T) {
	w := newWorld(t)
	w.keeper()
	w.keeperJSON(state.LinkRestarting, state.KindCCY, new((boot0 + 4*time.Second).Milliseconds()))
	l := w.listen()
	w.stdin = strings.NewReader("hi")
	if code := w.do("say", "--to", "bob"); code != exits.ExitNoRuntime {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	l.request(t)
	e := w.errorObject()
	if e.Code != exits.CodeNoLink || !strings.Contains(e.Message, "may already") || !strings.Contains(e.Message, "status") {
		t.Fatalf("error %+v", e)
	}
}
