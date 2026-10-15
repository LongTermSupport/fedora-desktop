package systemd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestUnitActiveReadsTheExitStatus(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "is-active", "--quiet", "vsftpd"}, execx.Result{})
	s := &Systemd{Runner: f}
	up, err := s.UnitActive(context.Background(), "vsftpd")
	if err != nil || !up {
		t.Fatalf("active unit: %v %v", up, err)
	}
	f.On([]string{"systemctl", "is-active", "--quiet", "vsftpd"}, execx.Result{ExitCode: 3})
	up, err = s.UnitActive(context.Background(), "vsftpd")
	if err != nil || up {
		t.Fatalf("inactive unit: %v %v", up, err)
	}
}

func TestUnitActiveSystemctlThatCannotRunIsAnError(t *testing.T) {
	f := &execx.Fake{}
	f.OnError([]string{"systemctl"}, &execx.NotInstalledError{Name: "systemctl"})
	if _, err := (&Systemd{Runner: f}).UnitActive(context.Background(), "vsftpd"); !execx.IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}

func rootFor(f *execx.Fake) *sudo.Sudo {
	return &sudo.Sudo{
		Runner: f,
		Term:   &term.Term{Err: &bytes.Buffer{}, IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" }},
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
	}
}

func TestRootVerbsRunSystemctlThroughSudo(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "systemctl"}, execx.Result{})
	s := &Systemd{Runner: f}
	root := rootFor(f)
	ctx := context.Background()
	if err := s.StartAsRoot(ctx, root, "vsftpd"); err != nil {
		t.Fatal(err)
	}
	if err := s.StopAsRoot(ctx, root, "vsftpd"); err != nil {
		t.Fatal(err)
	}
	if err := s.DaemonReloadAsRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo -n systemctl start vsftpd", "sudo -n systemctl stop vsftpd", "sudo -n systemctl daemon-reload"}
	calls := f.Calls()
	for i, w := range want {
		if calls[i].Line() != w {
			t.Errorf("call %d: %q, want %q", i, calls[i].Line(), w)
		}
	}
}

func TestRootVerbFailureNamesTheVerb(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "systemctl"}, execx.Result{ExitCode: 5})
	err := (&Systemd{Runner: f}).StartAsRoot(context.Background(), rootFor(f), "vsftpd")
	var exitErr *execx.ExitError
	if !errors.As(err, &exitErr) || !strings.Contains(err.Error(), "systemctl start vsftpd") {
		t.Fatalf("got %v", err)
	}
}
