package tmuxccy_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

var (
	listArgs    = []string{"tmux", "-L", "ccy", "list-sessions"}
	clientsArgs = []string{"tmux", "-L", "ccy", "list-clients", "-t", "=alpha"}
	attachArgs  = []string{"tmux", "-L", "ccy", "attach-session", "-t", "=alpha"}
	hookArgs    = []string{"tmux", "-L", "ccy", "set-hook", "-g", "client-attached"}
)

// newTerm is a Term whose lines land in the returned buffer; tty says whether it is a terminal.
func newTerm(tty bool) (*term.Term, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &term.Term{Err: buf, IsTerminal: func(any) bool { return tty }, Getenv: func(string) string { return "" }}, buf
}

func newTmux(f execx.Runner) *tmuxccy.Tmux {
	tm, _ := newTerm(false)
	return &tmuxccy.Tmux{Runner: f, Term: tm}
}

func fail(msg string) execx.Result {
	return execx.Result{Stderr: []byte(msg), ExitCode: 1}
}

func TestListParsesSessions(t *testing.T) {
	f := &execx.Fake{}
	f.On(listArgs, execx.Result{Stdout: []byte("ccy-proj 1 /srv/my proj\nccy-proj-2 0 /srv/other\n\n")})
	got, err := newTmux(f).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []tmuxccy.Session{
		{Name: "ccy-proj", Attached: 1, Path: "/srv/my proj"},
		{Name: "ccy-proj-2", Attached: 0, Path: "/srv/other"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	call := f.Calls()[0]
	if call.Line() != "tmux -L ccy list-sessions -F #{session_name} #{session_attached} #{session_path}" {
		t.Fatalf("argv %q", call.Line())
	}
}

func TestListNoServerIsEmpty(t *testing.T) {
	for _, msg := range []string{
		"no server running on /tmp/tmux-1000/ccy",
		"error connecting to /tmp/tmux-1000/ccy (No such file or directory)",
	} {
		f := &execx.Fake{}
		f.On(listArgs, fail(msg))
		got, err := newTmux(f).List(context.Background())
		if err != nil || len(got) != 0 {
			t.Fatalf("%q: got %v, %v", msg, got, err)
		}
	}
}

func TestListRealFailureIsError(t *testing.T) {
	f := &execx.Fake{}
	f.On(listArgs, fail("protocol version mismatch"))
	sessions, err := newTmux(f).List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "protocol version mismatch") || sessions != nil {
		t.Fatalf("err %v", err)
	}
}

func TestListStartFailureIsError(t *testing.T) {
	tm := newTmux(errRunner{})
	if sessions, err := tm.List(context.Background()); err == nil || sessions != nil {
		t.Fatal("want error")
	}
}

func TestListMalformedLine(t *testing.T) {
	for _, out := range []string{"onlyname\n", "name x /p\n"} {
		f := &execx.Fake{}
		f.On(listArgs, execx.Result{Stdout: []byte(out)})
		if sessions, err := newTmux(f).List(context.Background()); err == nil || sessions != nil {
			t.Fatalf("%q: want error", out)
		}
	}
}

func TestClients(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{Stdout: []byte("/dev/pts/3 1700000000\n/dev/pts/4 1700000100\n")})
	got, err := newTmux(f).Clients(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TTY != "/dev/pts/3" || got[1].Activity != 1700000100 {
		t.Fatalf("got %+v", got)
	}
	if f.Calls()[0].Line() != "tmux -L ccy list-clients -t =alpha -F #{client_tty} #{client_activity}" {
		t.Fatalf("argv %q", f.Calls()[0].Line())
	}
}

func TestClientsEmpty(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{})
	got, err := newTmux(f).Clients(context.Background(), "alpha")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestClientsMalformed(t *testing.T) {
	for _, out := range []string{"/dev/pts/3\n", "/dev/pts/3 soon\n"} {
		f := &execx.Fake{}
		f.On(clientsArgs, execx.Result{Stdout: []byte(out)})
		if clients, err := newTmux(f).Clients(context.Background(), "alpha"); err == nil || clients != nil {
			t.Fatalf("%q: want error", out)
		}
	}
}

func TestClientsGone(t *testing.T) {
	for _, msg := range []string{"can't find session: alpha", "no server running on x", "x (No such file or directory)"} {
		f := &execx.Fake{}
		f.On(clientsArgs, fail(msg))
		clients, err := newTmux(f).Clients(context.Background(), "alpha")
		if !errors.Is(err, tmuxccy.ErrSessionGone) || clients != nil {
			t.Fatalf("%q: err %v", msg, err)
		}
	}
}

func TestClientsRealFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, fail("boom"))
	clients, err := newTmux(f).Clients(context.Background(), "alpha")
	if err == nil || errors.Is(err, tmuxccy.ErrSessionGone) || !strings.Contains(err.Error(), "boom") || clients != nil {
		t.Fatalf("err %v", err)
	}
}

func TestIsDetached(t *testing.T) {
	cases := []struct {
		name string
		res  execx.Result
		want bool
		err  bool
	}{
		{"no clients", execx.Result{}, true, false},
		{"a client", execx.Result{Stdout: []byte("/dev/pts/3 1\n")}, false, false},
		{"gone", fail("can't find session: alpha"), false, false},
		{"failure", fail("boom"), false, true},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		f.On(clientsArgs, c.res)
		got, err := newTmux(f).IsDetached(context.Background(), "alpha")
		if got != c.want || (err != nil) != c.err {
			t.Fatalf("%s: got %v, %v", c.name, got, err)
		}
	}
}

func TestSingleAttachHook(t *testing.T) {
	want := `if-shell 'test $(tmux -L ccy list-clients -t "#{session_name}" | wc -l) -gt 1' 'detach-client'`
	if got := tmuxccy.SingleAttachHook(); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestInstallHook(t *testing.T) {
	f := &execx.Fake{}
	f.On(hookArgs, execx.Result{})
	if err := newTmux(f).InstallHook(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if len(c.Args) != 6 || c.Args[5] != tmuxccy.SingleAttachHook() {
		t.Fatalf("args %q", c.Args)
	}
}

func TestInstallHookFails(t *testing.T) {
	f := &execx.Fake{}
	f.On(hookArgs, fail("nope"))
	if err := newTmux(f).InstallHook(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestAttachDetachedAfterwards(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{})
	f.On(hookArgs, execx.Result{})
	f.On(attachArgs, execx.Result{})
	tm := newTmux(f)
	stderr, ok := tm.Term.Err.(*bytes.Buffer)
	if !ok {
		t.Fatal("term is not a buffer")
	}
	out, err := tm.Attach(context.Background(), "alpha")
	if err != nil || out != tmuxccy.Detached {
		t.Fatalf("got %v, %v", out, err)
	}
	if !strings.Contains(stderr.String(), "Attaching to 'alpha'.") ||
		!strings.Contains(stderr.String(), "Detached. 'alpha' keeps running") {
		t.Fatalf("stderr %q", stderr.String())
	}
	if !f.Called("tmux -L ccy attach-session -t =alpha") {
		t.Fatal("no attach")
	}
}

func TestAttachRefusesBusyOrGone(t *testing.T) {
	for _, res := range []execx.Result{{Stdout: []byte("/dev/pts/3 1\n")}, fail("can't find session: alpha")} {
		f := &execx.Fake{}
		f.On(clientsArgs, res)
		tm, said := newTerm(false)
		out, err := (&tmuxccy.Tmux{Runner: f, Term: tm}).Attach(context.Background(), "alpha")
		if !errors.Is(err, tmuxccy.ErrOpenElsewhere) || out != tmuxccy.Refused {
			t.Fatalf("err %v", err)
		}
		refused, ok := errors.AsType[*tmuxccy.RefusedError](err)
		if !ok || refused.Message != "'alpha' is open in another terminal, or gone. It can be attached from one terminal only." {
			t.Fatalf("refusal %v", err)
		}
		if said.Len() != 0 {
			t.Fatalf("Attach printed a refusal the caller will print: %q", said.String())
		}
		if f.Called("tmux -L ccy attach-session") || f.Called("tmux -L ccy set-hook") {
			t.Fatal("must not attach")
		}
	}
}

func TestAttachDetachedCheckFails(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, fail("boom"))
	if out, err := newTmux(f).Attach(context.Background(), "alpha"); err == nil ||
		errors.Is(err, tmuxccy.ErrOpenElsewhere) || out != tmuxccy.Refused {
		t.Fatalf("err %v", err)
	}
}

func TestAttachHookFails(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{})
	f.On(hookArgs, fail("nope"))
	if out, err := newTmux(f).Attach(context.Background(), "alpha"); err == nil || out != tmuxccy.Refused {
		t.Fatal("want error")
	}
	if f.Called("tmux -L ccy attach-session") {
		t.Fatal("must not attach")
	}
}

func TestAttachFails(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{})
	f.On(hookArgs, execx.Result{})
	f.On(attachArgs, fail("open terminal failed"))
	out, err := newTmux(f).Attach(context.Background(), "alpha")
	if err == nil || !strings.Contains(err.Error(), "could not attach to 'alpha'") || out != tmuxccy.Refused {
		t.Fatalf("err %v", err)
	}
}

// sequenceRunner answers list-clients from a queue so the before and after views differ.
type sequenceRunner struct {
	clients []execx.Result
	n       int
}

func (s *sequenceRunner) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	if len(c.Args) > 2 && c.Args[2] == "list-clients" {
		r := s.clients[s.n]
		s.n++
		if r.ExitCode != 0 {
			return r, &execx.ExitError{Name: c.Name, Code: r.ExitCode, Stderr: string(r.Stderr)}
		}
		return r, nil
	}
	return execx.Result{}, nil
}

func TestAttachEnded(t *testing.T) {
	r := &sequenceRunner{clients: []execx.Result{{}, fail("can't find session: alpha")}}
	tm := newTmux(r)
	stderr, ok := tm.Term.Err.(*bytes.Buffer)
	if !ok {
		t.Fatal("term is not a buffer")
	}
	out, err := tm.Attach(context.Background(), "alpha")
	if err != nil || out != tmuxccy.Ended {
		t.Fatalf("got %v, %v", out, err)
	}
	if !strings.Contains(stderr.String(), "'alpha' has ended.") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestAttachKickedOnArrival(t *testing.T) {
	r := &sequenceRunner{clients: []execx.Result{{}, {Stdout: []byte("/dev/pts/9 1\n")}}}
	tm, stderr := newTerm(false)
	out, err := (&tmuxccy.Tmux{Runner: r, Term: tm}).Attach(context.Background(), "alpha")
	if err != nil || out != tmuxccy.Kicked {
		t.Fatalf("got %v, %v", out, err)
	}
	want := "Attaching to 'alpha'.\nERROR: 'alpha' is open in another terminal, so this terminal was not attached to it.\n"
	if stderr.String() != want {
		t.Fatalf("stderr %q", stderr.String())
	}
	coloured, colouredOut := newTerm(true)
	r2 := &sequenceRunner{clients: []execx.Result{{}, {Stdout: []byte("/dev/pts/9 1\n")}}}
	if _, err := (&tmuxccy.Tmux{Runner: r2, Term: coloured}).Attach(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colouredOut.String(), "\x1b[0;31mERROR:\x1b[0m") {
		t.Fatalf("no colour on a terminal: %q", colouredOut.String())
	}
}

func TestAttachAfterListFails(t *testing.T) {
	r := &sequenceRunner{clients: []execx.Result{{}, fail("boom")}}
	tm := newTmux(r)
	if out, err := tm.Attach(context.Background(), "alpha"); err == nil || out != tmuxccy.Refused {
		t.Fatal("want error")
	}
}

func TestAttachNilStderr(t *testing.T) {
	f := &execx.Fake{}
	f.On(clientsArgs, execx.Result{})
	f.On(hookArgs, execx.Result{})
	f.On(attachArgs, execx.Result{})
	if out, err := newTmux(f).Attach(context.Background(), "alpha"); err != nil || out != tmuxccy.Detached {
		t.Fatal(err)
	}
}

func TestAttachEmptyName(t *testing.T) {
	if out, err := newTmux(&execx.Fake{}).Attach(context.Background(), ""); err == nil || out != tmuxccy.Refused {
		t.Fatal("want error")
	}
}

func TestRunPassesThrough(t *testing.T) {
	f := &execx.Fake{}
	f.On(listArgs, execx.Result{Stdout: []byte("x")})
	res, err := newTmux(f).Run(context.Background(), "list-sessions")
	if err != nil || string(res.Stdout) != "x" {
		t.Fatalf("got %v, %v", res, err)
	}
}

type errRunner struct{}

func (errRunner) Run(context.Context, execx.Cmd) (execx.Result, error) {
	return execx.Result{}, errors.New("tmux: not installed")
}
