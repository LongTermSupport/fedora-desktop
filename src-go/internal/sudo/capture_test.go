package sudo

import (
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestCaptureRunsSudoNAndKeepsBothStreamsSeparate(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{Stdout: []byte("a\nb\n"), Stderr: []byte("sudo: unable to resolve host\n")})
	s := newSudo(f, true)
	res, err := s.Capture(context.Background(), "lxc-ls", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "a\nb\n" || string(res.Stderr) != "sudo: unable to resolve host\n" {
		t.Errorf("streams %q / %q", res.Stdout, res.Stderr)
	}
	if said(s) != "" {
		t.Errorf("Capture must not copy stderr to the terminal, wrote %q", said(s))
	}
}

func TestCaptureFailureReturnsTheResultWithItsStderr(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-info"}, execx.Result{Stderr: []byte("lxc-info: no such container\n"), ExitCode: 1})
	s := newSudo(f, true)
	res, err := s.Capture(context.Background(), "lxc-info")
	var exitErr *execx.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("want the exit error, got %v", err)
	}
	if string(res.Stderr) != "lxc-info: no such container\n" || said(s) != "" {
		t.Errorf("stderr %q, terminal %q", res.Stderr, said(s))
	}
	if errors.Is(err, ErrNeedsPassword) {
		t.Error("a command's own failure is not a password prompt")
	}
}

func TestCapturePasswordWantedIsErrNeedsPassword(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n"}, execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	s := newSudo(f, true)
	_, err := s.Capture(context.Background(), "true")
	if !errors.Is(err, ErrNeedsPassword) {
		t.Fatalf("got %v", err)
	}
}
