package execx

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The tests below send real signals to this test process; none runs in parallel with another.

// readyPipe is a pipe the child writes "ready" on once its traps are set, and the reader that
// waits for it.
func readyPipe(t *testing.T) (*os.File, *bufio.Reader) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return w, bufio.NewReader(r)
}

type outcome struct {
	exit Exit
	err  error
}

func foreground(c Cmd) <-chan outcome {
	done := make(chan outcome, 1)
	go func() {
		exit, err := OS{}.Foreground(c)
		done <- outcome{exit, err}
	}()
	return done
}

func waitFor(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != want {
		t.Fatalf("the child said %q, %v; want %q", line, err, want)
	}
}

func signalSelf(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Fatal(err)
	}
}

func closeWriter(t *testing.T, w *os.File) {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// The child stays in this process's group, the terminal's foreground group, with its own
// children: a terminal's Ctrl-C reaches it as it reached the bash launcher's children.
func TestForegroundKeepsTheChildInTheForegroundProcessGroup(t *testing.T) {
	var out bytes.Buffer
	script := `read -r _ _ _ _ pgid _ </proc/$$/stat; echo "$pgid"; sh -c 'read -r _ _ _ _ pgid _ </proc/$$/stat; echo "$pgid"'`
	exit, err := OS{}.Foreground(Cmd{Name: "sh", Args: []string{"-c", script}, Stdout: &out})
	if err != nil || exit.Code != 0 {
		t.Fatalf("Foreground = %+v, %v", exit, err)
	}
	want := strconv.Itoa(syscall.Getpgrp())
	if got := strings.Fields(out.String()); !slices.Equal(got, []string{want, want}) {
		t.Fatalf("the child and its child are in groups %q, want both in %s", got, want)
	}
}

func TestForegroundForwardsTERMToTheChildAndReturnsItsStatus(t *testing.T) {
	w, ready := readyPipe(t)
	done := foreground(Cmd{Name: "sh", Args: []string{"-c", `trap 'echo got-term; exit 7' TERM; echo ready; while :; do sleep 0.05; done`}, Stdout: w})
	waitFor(t, ready, "ready")
	signalSelf(t, syscall.SIGTERM)
	got := <-done
	closeWriter(t, w)
	if got.err != nil || got.exit.Code != 7 || got.exit.Signal != 0 {
		t.Fatalf("Foreground = %+v, %v; want the child's own exit 7", got.exit, got.err)
	}
	if !slices.Contains(got.exit.Received, os.Signal(syscall.SIGTERM)) {
		t.Fatalf("Received = %v, want SIGTERM", got.exit.Received)
	}
	waitFor(t, ready, "got-term")
}

func TestForegroundForwardsHUPAndReportsAChildKilledByIt(t *testing.T) {
	w, ready := readyPipe(t)
	done := foreground(Cmd{Name: "sh", Args: []string{"-c", `echo ready; exec sleep 30`}, Stdout: w})
	waitFor(t, ready, "ready")
	signalSelf(t, syscall.SIGHUP)
	got := <-done
	closeWriter(t, w)
	if got.err != nil || got.exit.Code != 128+int(syscall.SIGHUP) || got.exit.Signal != syscall.SIGHUP {
		t.Fatalf("Foreground = %+v, %v; want killed by SIGHUP, code 129", got.exit, got.err)
	}
}

// The terminal delivers Ctrl-C to the whole foreground group, the child included, so the
// launcher neither dies of it nor sends the child a second one.
func TestForegroundOutlivesINTAndDoesNotForwardIt(t *testing.T) {
	w, ready := readyPipe(t)
	in, feed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := foreground(Cmd{
		Name: "sh", Args: []string{"-c", `trap 'echo got-int' INT; echo ready; read -r line; echo "line=$line"`},
		Stdin: in, Stdout: w,
	})
	waitFor(t, ready, "ready")
	signalSelf(t, syscall.SIGINT)
	signalSelf(t, syscall.SIGQUIT)
	if _, err := feed.WriteString("done\n"); err != nil {
		t.Fatal(err)
	}
	closeWriter(t, feed)
	got := <-done
	closeWriter(t, w)
	closeWriter(t, in)
	if got.err != nil || got.exit.Code != 0 {
		t.Fatalf("Foreground = %+v, %v; want the child to finish", got.exit, got.err)
	}
	waitFor(t, ready, "line=done")
}

func TestForegroundOfAProgramThatIsNotInstalled(t *testing.T) {
	_, err := OS{}.Foreground(Cmd{Name: "ccy-no-such-program"})
	if !IsNotInstalled(err) {
		t.Fatalf("Foreground error = %v, want not installed", err)
	}
	_, err = OS{}.Foreground(Cmd{Name: "/", Dir: "/"})
	if err == nil || IsNotInstalled(err) {
		t.Fatalf("Foreground of a directory = %v, want a start error", err)
	}
}

func TestForegroundInheritsTheStreamsByDefault(t *testing.T) {
	exit, err := OS{}.Foreground(Cmd{Name: "true", Dir: t.TempDir(), Env: []string{"CCY_TEST=1"}})
	if err != nil || exit.Code != 0 {
		t.Fatalf("Foreground = %+v, %v", exit, err)
	}
}

func TestForegroundPassesTheSecretAndTheEnvironment(t *testing.T) {
	var out bytes.Buffer
	s := mustSecret(t, "CCY_TEST_TOKEN")
	exit, err := OS{}.Foreground(Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s %s' "$CCY_TEST_TOKEN" "$CCY_TEST_PLAIN"`},
		Env: []string{"CCY_TEST_PLAIN=plain"}, Secrets: []Secret{s}, Stdout: &out,
	})
	if err != nil || exit.Code != 0 || out.String() != placeholder+" plain" {
		t.Fatalf("Foreground = %+v, %v, %q", exit, err, out.String())
	}
}

func TestFakeForeground(t *testing.T) {
	f := &Fake{}
	f.On([]string{"podman", "run"}, Result{ExitCode: 75})
	exit, err := f.Foreground(Cmd{Name: "podman", Args: []string{"run", "x"}})
	if err != nil || exit.Code != 75 {
		t.Fatalf("Fake.Foreground = %+v, %v", exit, err)
	}
	if got := f.Foregrounds(); len(got) != 1 || got[0].Line() != "podman run x" {
		t.Fatalf("Foregrounds = %v", got)
	}
	if _, err := f.Foreground(Cmd{Name: "docker"}); !errors.Is(err, ErrUnexpected) {
		t.Fatalf("an unscripted foreground = %v, want ErrUnexpected", err)
	}
	f.OnError([]string{"podman"}, &NotInstalledError{Name: "podman"})
	if _, err := f.Foreground(Cmd{Name: "podman"}); !IsNotInstalled(err) {
		t.Fatalf("a scripted start failure = %v", err)
	}
	s := mustSecret(t, "GH_TOKEN")
	if _, err := f.Foreground(Cmd{Name: "podman", Args: []string{placeholder}, Secrets: []Secret{s}}); !errors.Is(err, ErrSecretInArgv) {
		t.Fatalf("Fake.Foreground with the secret in argv = %v", err)
	}
	f.OnFunc([]string{"gh"}, func(Cmd) (Result, error) { return Result{}, errors.New("boom") })
	if _, err := f.Foreground(Cmd{Name: "gh"}); err == nil {
		t.Fatal("a scripted failure was lost")
	}
}

func TestExitOfAWaitThatFailedForAnotherReason(t *testing.T) {
	broken := errors.New("wait failed")
	if _, err := exitOf("x", broken, nil); !errors.Is(err, broken) {
		t.Fatalf("exitOf = %v", err)
	}
}
