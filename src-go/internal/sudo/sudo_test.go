package sudo

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func newSudo(f *execx.Fake, tty bool) *Sudo {
	return &Sudo{
		Runner: f,
		Term: &term.Term{
			Err:        &bytes.Buffer{},
			IsTerminal: func(any) bool { return tty },
			Getenv:     func(string) string { return "" },
		},
		Stdin: strings.NewReader(""),
	}
}

// said is what the Sudo wrote to its terminal.
func said(s *Sudo) string {
	buf, ok := s.Term.Err.(*bytes.Buffer)
	if !ok {
		return "not a buffer"
	}
	return buf.String()
}

func TestReadyCachedCredentialAsksNothing(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	s := newSudo(f, true)
	if err := s.Ready(context.Background(), "Why."); err != nil {
		t.Fatal(err)
	}
	if said(s) != "" || len(f.Calls()) != 1 {
		t.Errorf("said %q, calls %v", said(s), f.Calls())
	}
}

func TestReadyNoTerminalRefusesWithProbe(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	s := newSudo(f, false)
	err := s.Ready(context.Background(), "Backing up.")
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("got %v", err)
	}
	want := "ERROR: Backing up. That needs sudo, and with no terminal there is nowhere to ask for your password (sudo said: sudo: a password is required). Run sudo -v first.\n"
	if said(s) != want {
		t.Errorf("got %q", said(s))
	}
	if f.Called("sudo -v") {
		t.Error("asked for a password with no terminal")
	}
}

func TestReadyAsksOnTerminal(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{ExitCode: 1})
	f.On([]string{"sudo", "-v"}, execx.Result{})
	s := newSudo(f, true)
	if err := s.Ready(context.Background(), "Removing."); err != nil {
		t.Fatal(err)
	}
	want := "\n\x1b[1mRemoving.\x1b[0m\nsudo asks for your login password.\n"
	if said(s) != want {
		t.Errorf("got %q", said(s))
	}
	c := f.Calls()[1]
	if c.Line() != "sudo -v" || c.Stdin != s.Stdin || c.Stderr != s.Term.Err {
		t.Errorf("sudo -v not attached to the terminal: %+v", c)
	}
}

func TestReadyRefusedPassword(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{ExitCode: 1})
	f.On([]string{"sudo", "-v"}, execx.Result{ExitCode: 1})
	s := newSudo(f, true)
	if err := s.Ready(context.Background(), "x"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(said(s), "ERROR:\x1b[0m sudo was not given your password (three wrong tries, or cancelled).") {
		t.Errorf("got %q", said(s))
	}
}

func TestAsRootRunsSudoN(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", AgentBus, "list", "alpha"}, execx.Result{Stdout: []byte("member\n")})
	out, err := newSudo(f, true).AsRoot(context.Background(), AgentBus, "list", "alpha")
	if err != nil || out != "member\n" {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestAsRootPasswordWanted(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n"}, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	s := newSudo(f, true)
	out, err := s.AsRoot(context.Background(), AgentBus, "list", "alpha")
	if !errors.Is(err, ErrNeedsPassword) || out != "" {
		t.Fatalf("out %q err %v", out, err)
	}
	if said(s) != "" {
		t.Errorf("printed %q", said(s))
	}
}

func TestAsRootCommandFailureCopiesStderr(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n"}, execx.Result{Stderr: []byte("agent-bus: no such team\n"), ExitCode: 1})
	s := newSudo(f, true)
	out, err := s.AsRoot(context.Background(), AgentBus, "list", "zzz")
	var exitErr *execx.ExitError
	if !errors.As(err, &exitErr) || errors.Is(err, ErrNeedsPassword) || out != "" {
		t.Fatalf("out %q err %v", out, err)
	}
	if said(s) != "agent-bus: no such team\n" {
		t.Errorf("stderr %q", said(s))
	}
}

func TestAsRootOtherExitCodeIsFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n"}, execx.Result{Stderr: []byte("sudo: boom\n"), ExitCode: 2})
	out, err := newSudo(f, true).AsRoot(context.Background(), InstallTool)
	if errors.Is(err, ErrNeedsPassword) || err == nil || out != "" {
		t.Fatalf("exit 2 is a failure, got out %q err %v", out, err)
	}
}

func TestAsRootCouldNotStart(t *testing.T) {
	out, err := newSudo(&execx.Fake{}, true).AsRoot(context.Background(), AgentBus)
	if !errors.Is(err, execx.ErrUnexpected) || out != "" {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestRunAfterReadyUsesSudoN(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", InstallTool}, execx.Result{})
	s := newSudo(f, true)
	if err := s.Run(context.Background(), InstallTool, "backup"); err != nil {
		t.Fatal(err)
	}
	c := f.Calls()[0]
	if c.Line() != "sudo -n "+InstallTool+" backup" || c.Stdout != s.Term.Err || c.Stderr != s.Term.Err {
		t.Errorf("got %+v", c)
	}
}

func TestRunFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n"}, execx.Result{ExitCode: 78})
	var exitErr *execx.ExitError
	if err := newSudo(f, true).Run(context.Background(), InstallTool); !errors.As(err, &exitErr) || exitErr.Code != 78 {
		t.Fatalf("got %v", err)
	}
}

func TestNewWiresRealStreams(t *testing.T) {
	if s := New(&execx.Fake{}, term.New()); s.Stdin == nil || s.Runner == nil {
		t.Fatal("New leaves a field unset")
	}
}
