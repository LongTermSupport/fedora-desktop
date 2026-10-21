package execx

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// NewSession starts the child as a session leader, so its pid is its process group id (the
// status line's inner command relies on it to kill everything it started with one signal).
func TestOSRunnerNewSessionMakesTheChildItsOwnGroup(t *testing.T) {
	res, err := (OS{}).Run(context.Background(), Cmd{
		Name:       "sh",
		Args:       []string{"-c", `echo $$; cut -d' ' -f5 /proc/$$/stat`},
		NewSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(res.Stdout))
	if len(lines) != 2 || lines[0] != lines[1] {
		t.Fatalf("want the shell's pid as its process group, got %q", res.Stdout)
	}
}

// A cancel kills the whole group, grandchildren included, so nothing it started outlives the run.
func TestOSRunnerCancelKillsTheWholeNewSessionGroup(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (OS{}).Run(ctx, Cmd{
			Name:       "sh",
			Args:       []string{"-c", "sleep 30 & echo $! > " + filepath.Join(dir, "grandchild") + "; wait"},
			NewSession: true,
		})
		done <- err
	}()
	pidDir := os.DirFS(dir)
	for range 500 {
		if raw, err := fs.ReadFile(pidDir, "grandchild"); err == nil && len(raw) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("want the cancelled run to fail")
	}
	raw, err := fs.ReadFile(pidDir, "grandchild")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(os.DirFS("/proc"), strings.TrimSpace(string(raw))); err == nil {
		t.Fatal("the grandchild outlived the cancel")
	}
}

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

func TestOSRunnerNamesTheSignalThatEndedTheCommand(t *testing.T) {
	_, err := OS{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "kill -TERM $$"}})
	exit, ok := errors.AsType[*ExitError](err)
	if !ok {
		t.Fatalf("want *ExitError, got %v", err)
	}
	if exit.Signal != syscall.SIGTERM || exit.Code != -1 {
		t.Fatalf("signal %v code %d", exit.Signal, exit.Code)
	}
	if exit.Error() != "sh killed by SIGTERM" {
		t.Fatalf("error text %q", exit.Error())
	}
}

func TestOSRunnerAnExitIsNoSignal(t *testing.T) {
	_, err := OS{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "exit 130"}})
	if exit, ok := errors.AsType[*ExitError](err); !ok || exit.Signal != 0 || exit.Code != 130 {
		t.Fatalf("got %#v", err)
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

func TestOSRunnerHandsExtraFilesFromDescriptorThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extra")
	if err := os.WriteFile(path, []byte("inherited"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Error(cerr)
		}
	}()
	res, err := OS{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "cat <&3"}, ExtraFiles: []*os.File{f}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "inherited" {
		t.Fatalf("descriptor 3 read %q", res.Stdout)
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
