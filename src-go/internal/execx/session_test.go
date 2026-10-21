package execx

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sessionOutcome is what a NewSession run started by startSession returned.
type sessionOutcome struct {
	res Result
	err error
}

func startSession(ctx context.Context, r OS, c Cmd) <-chan sessionOutcome {
	c.NewSession = true
	done := make(chan sessionOutcome, 1)
	go func() {
		res, err := r.Run(ctx, c)
		done <- sessionOutcome{res, err}
	}()
	return done
}

// pidFrom waits for a child to write its pid to the file, and returns it.
func pidFrom(t *testing.T, path string) int {
	t.Helper()
	for range 1000 {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err == nil && bytes.HasSuffix(raw, []byte("\n")) {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr != nil {
				t.Fatal(convErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", path)
	return 0
}

// living is whether pid is a process that has not ended (a zombie has ended).
func living(t *testing.T, pid int) bool {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS("/proc"), strconv.Itoa(pid)+"/stat")
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(raw[bytes.LastIndexByte(raw, ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}

// dies waits up to five seconds for pid to end (SIGKILL is delivered, then the process is reaped).
func dies(t *testing.T, pid int) bool {
	t.Helper()
	for range 500 {
		if !living(t, pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestSessionChildIsItsOwnProcessGroup(t *testing.T) {
	res, err := (OS{}).Run(context.Background(), Cmd{
		Name:       "sh",
		Args:       []string{"-c", `echo $$; cut -d' ' -f5 /proc/$$/stat`},
		NewSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(res.Stdout))
	if len(fields) != 2 || fields[0] != fields[1] {
		t.Fatalf("want the shell's pid as its process group, got %q", res.Stdout)
	}
}

// Like Python's communicate, the run lasts until stdout closes, so a grandchild that still holds
// it is waited for after the shell itself has exited.
func TestSessionWaitsForAGrandchildHoldingStdout(t *testing.T) {
	res, err := (OS{}).Run(context.Background(), Cmd{
		Name:       "sh",
		Args:       []string{"-c", "echo first; (sleep 0.3; echo second) &"},
		NewSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "first\nsecond\n" {
		t.Fatalf("stdout %q", res.Stdout)
	}
}

func TestSessionFeedsStdinAndCapturesStderr(t *testing.T) {
	res, err := (OS{}).Run(context.Background(), Cmd{
		Name:       "sh",
		Args:       []string{"-c", "cat; echo oops >&2"},
		Stdin:      strings.NewReader("payload"),
		NewSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "payload" || string(res.Stderr) != "oops\n" {
		t.Fatalf("stdout %q stderr %q", res.Stdout, res.Stderr)
	}
}

func TestSessionStreamsToTheGivenWriters(t *testing.T) {
	var out, errOut bytes.Buffer
	file, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	res, err := (OS{}).Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", "echo to-out; echo to-err >&2"},
		Stdout: &out, Stderr: &errOut, NewSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "to-out\n" || errOut.String() != "to-err\n" || len(res.Stdout) != 0 {
		t.Fatalf("stdout %q stderr %q result %q", out.String(), errOut.String(), res.Stdout)
	}
	if _, err = (OS{}).Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", "echo direct >&2"}, Stderr: file, NewSession: true,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file.Name())
	if err != nil || string(raw) != "direct\n" {
		t.Fatalf("the file stderr holds %q (%v)", raw, err)
	}
}

// A child that never reads its stdin, given more than a pipe holds, still ends the run.
func TestSessionAChildThatIgnoresStdinStillEnds(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 1<<20)
	if _, err := (OS{}).Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", "exit 0"}, Stdin: bytes.NewReader(big), NewSession: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionReportsTheExitStatusAndTheSignal(t *testing.T) {
	_, err := (OS{}).Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "exit 3"}, NewSession: true})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 || exit.Signal != 0 {
		t.Fatalf("want exit 3, got %v", err)
	}
	res, err := (OS{}).Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "kill -TERM $$"}, NewSession: true})
	if !errors.As(err, &exit) || exit.Signal != syscall.SIGTERM || res.ExitCode != -1 {
		t.Fatalf("want a SIGTERM death, got %v (code %d)", err, res.ExitCode)
	}
}

func TestSessionAProgramThatCannotStartIsNotAnExit(t *testing.T) {
	_, err := (OS{}).Run(context.Background(), Cmd{Name: "/nonexistent/program", NewSession: true})
	var exit *ExitError
	if err == nil || errors.As(err, &exit) {
		t.Fatalf("want a start failure, got %v", err)
	}
	if _, err := (OS{}).Run(context.Background(), Cmd{Name: "sh", Dir: "/nonexistent/dir", NewSession: true}); err == nil {
		t.Fatal("want a start failure for a missing directory")
	}
}

// A run that cannot start closes the stdin pipe it made as well as the child's ends: nothing
// will ever feed it, and an open write end is a descriptor lost per failed start.
func TestSessionAFailedStartClosesTheStdinPipe(t *testing.T) {
	pipes := &sessionPipes{}
	c := Cmd{Name: "/nonexistent/program", Stdin: strings.NewReader("payload"), NewSession: true}
	if _, err := (OS{}).session(context.Background(), c, pipes); err == nil {
		t.Fatal("want a start failure")
	}
	if pipes.stdin == nil {
		t.Fatal("no stdin pipe was made")
	}
	if _, err := pipes.stdin.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("the stdin pipe is still open: write gave %v", err)
	}
	if err := pipes.closeReaders(); err != nil {
		t.Fatal(err)
	}
}

// A cancel kills the whole group at once, grandchildren included, and the run returns the cause.
func TestSessionCancelKillsTheWholeGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := startSession(ctx, OS{}, Cmd{
		Name: "sh", Args: []string{"-c", "sleep 30 & echo $! > " + pidFile + "; wait"},
	})
	pid := pidFrom(t, pidFile)
	cause := errors.New("the render was cancelled")
	cancel(cause)
	got := <-done
	if !errors.Is(got.err, cause) {
		t.Fatalf("want the cancel's cause, got %v", got.err)
	}
	if !dies(t, pid) {
		t.Fatal("the grandchild outlived the cancel")
	}
}

// The deadline reaches a grandchild still holding stdout after the shell itself exited: the
// group is killed then too (Python's killpg after its communicate timed out).
func TestSessionDeadlineKillsTheGroupAfterTheShellExited(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := startSession(ctx, OS{}, Cmd{
		Name: "sh", Args: []string{"-c", "echo early; sleep 30 & echo $! > " + pidFile},
	})
	pid := pidFrom(t, pidFile)
	cause := errors.New("timed out")
	cancel(cause)
	got := <-done
	if !errors.Is(got.err, cause) {
		t.Fatalf("want the deadline's cause, got %v", got.err)
	}
	if !dies(t, pid) {
		t.Fatal("the grandchild outlived the deadline")
	}
}

// A process that left the group cannot be killed with it; if it holds stdout, the run still ends
// after Grace instead of waiting on it.
func TestSessionDoesNotWaitPastGraceForAnEscapedHolder(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "escaped")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := startSession(ctx, OS{Grace: 50 * time.Millisecond}, Cmd{
		Name: "sh", Args: []string{"-c", "setsid sh -c 'echo $$ > " + pidFile + "; exec sleep 30' & wait"},
	})
	pid := pidFrom(t, pidFile)
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error(err)
		}
	})
	cause := errors.New("cancelled")
	cancel(cause)
	got := <-done
	if !errors.Is(got.err, cause) {
		t.Fatalf("want the cancel's cause, got %v", got.err)
	}
}

// A context already done never starts the program.
func TestSessionDoneContextStartsNothing(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (OS{}).Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "touch " + marker}, NewSession: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want the cancel, got %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the program ran: %v", err)
	}
}
