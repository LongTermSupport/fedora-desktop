package execx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOSRunnerCapturesOutputAndExitCode(t *testing.T) {
	r := OS{}
	res, err := r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}})
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("want *ExitError, got %v", err)
	}
	if exit.Code != 3 || res.ExitCode != 3 {
		t.Fatalf("want exit 3, got %d / %d", exit.Code, res.ExitCode)
	}
	if string(res.Stdout) != "out\n" || string(res.Stderr) != "err\n" {
		t.Fatalf("captured stdout %q stderr %q", res.Stdout, res.Stderr)
	}
	if !strings.Contains(exit.Error(), "sh exited 3") {
		t.Fatalf("error text %q", exit.Error())
	}
}

func TestOSRunnerPassesStdinEnvAndDir(t *testing.T) {
	dir := t.TempDir()
	res, err := OS{}.Run(context.Background(), Cmd{
		Name:  "sh",
		Args:  []string{"-c", `printf '%s|%s|' "$EXTRA" "$(pwd)"; cat`},
		Env:   []string{"EXTRA=set"},
		Dir:   dir,
		Stdin: strings.NewReader("piped"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(res.Stdout), "set|"+dir+"|piped"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOSRunnerStreamsWhenWritersGiven(t *testing.T) {
	var out bytes.Buffer
	res, err := OS{}.Run(context.Background(), Cmd{Name: "echo", Args: []string{"hi"}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "hi\n" || len(res.Stdout) != 0 {
		t.Fatalf("streamed %q, captured %q", out.String(), res.Stdout)
	}
}

func TestOSRunnerMissingCommandIsNotAnExitError(t *testing.T) {
	_, err := OS{}.Run(context.Background(), Cmd{Name: "no-such-command-for-execx-test"})
	var exit *ExitError
	if err == nil || errors.As(err, &exit) {
		t.Fatalf("want a start error, got %v", err)
	}
}

func TestFakeRecordsAndAnswersByPrefix(t *testing.T) {
	f := &Fake{}
	f.On([]string{"sudo", "-n", "agent-bus", "list"}, Result{Stdout: []byte("MEMBER\tx\n")})
	f.On([]string{"systemctl"}, Result{ExitCode: 1})

	res, err := f.Run(context.Background(), Cmd{Name: "sudo", Args: []string{"-n", "agent-bus", "list", "alpha"}})
	if err != nil || string(res.Stdout) != "MEMBER\tx\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
	_, err = f.Run(context.Background(), Cmd{Name: "systemctl", Args: []string{"is-active", "u"}})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	if _, err := f.Run(context.Background(), Cmd{Name: "gio"}); !errors.Is(err, ErrUnexpected) {
		t.Fatalf("want ErrUnexpected for an unscripted command, got %v", err)
	}
	if got := f.Calls(); len(got) != 3 || got[0].Line() != "sudo -n agent-bus list alpha" {
		t.Fatalf("calls %v", got)
	}
	if !f.Called("systemctl is-active") || f.Called("flatpak") {
		t.Fatal("Called matches by line prefix")
	}
}
