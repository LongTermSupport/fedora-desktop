package proc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOSRunsACommandAndSplitsTheStreams(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv: []string{"sh", "-c", "echo out; echo err >&2; exit 3"},
		Dir:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || res.Stdout != "out\n" || res.Stderr != "err\n" || res.TimedOut {
		t.Fatalf("got %+v", res)
	}
}

func TestOSFeedsTheCommandItsStandardInput(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:  []string{"cat"},
		Stdin: strings.NewReader("fed through stdin\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 || res.Stdout != "fed through stdin\n" {
		t.Fatalf("got %+v", res)
	}
}

func TestOSMergesTheStreamsInOrderWhenAsked(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:  []string{"sh", "-c", "echo one; echo two >&2; echo three"},
		Merge: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "one\ntwo\nthree\n" || res.Stderr != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestOSStreamsToTheWritersWhenGivenAndCapturesNothingThere(t *testing.T) {
	var out, errOut strings.Builder
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:     []string{"sh", "-c", "echo out; echo err >&2; exit 4"},
		StdoutTo: &out,
		StderrTo: &errOut,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "out\n" || errOut.String() != "err\n" {
		t.Fatalf("streamed %q and %q", out.String(), errOut.String())
	}
	if res.Code != 4 || res.Stdout != "" || res.Stderr != "" {
		t.Fatalf("a streamed stream is not also captured: %+v", res)
	}
}

func TestOSStreamsOneStreamAndCapturesTheOther(t *testing.T) {
	var out strings.Builder
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv:     []string{"sh", "-c", "echo out; echo err >&2"},
		StdoutTo: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "out\n" || res.Stderr != "err\n" || res.Stdout != "" {
		t.Fatalf("streamed %q, got %+v", out.String(), res)
	}
}

func TestOSPassesTheExtraEnvironment(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{
		Argv: []string{"sh", "-c", "printf %s \"$QA_RUN_PROBE\""},
		Env:  []string{"QA_RUN_PROBE=hello"},
	})
	if err != nil || res.Stdout != "hello" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestOSReportsAProgramThatCannotStart(t *testing.T) {
	if _, err := (OS{}).Run(context.Background(), Cmd{Argv: []string{"/no/such/program"}}); err == nil {
		t.Fatal("a missing program was not an error")
	}
	if _, err := (OS{}).Run(context.Background(), Cmd{}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty argv: %v", err)
	}
}

// gone is whether the process no longer runs: it has no /proc entry, or it is a zombie that
// only waits to be reaped.
func gone(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	// The state letter follows the parenthesised command name.
	rest := string(data[bytes.LastIndexByte(data, ')')+1:])
	return strings.HasPrefix(strings.TrimSpace(rest), "Z")
}

// deadlineAtLine is a context whose deadline passes when the command has written its first
// line, so no real timer races the line the test reads.
type deadlineAtLine struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	out  bytes.Buffer
}

func newDeadlineAtLine() *deadlineAtLine { return &deadlineAtLine{done: make(chan struct{})} }

// Deadline is none the runner can read ahead of time: the deadline is the line.
func (*deadlineAtLine) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*deadlineAtLine) Value(any) any { return nil }

func (d *deadlineAtLine) Done() <-chan struct{} { return d.done }

func (d *deadlineAtLine) Err() error {
	select {
	case <-d.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (d *deadlineAtLine) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, err := d.out.Write(p)
	if bytes.IndexByte(d.out.Bytes(), '\n') >= 0 {
		d.once.Do(func() { close(d.done) })
	}
	return n, err
}

func (d *deadlineAtLine) written() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.out.String()
}

// The command's background child holds its output pipe, so a kill of the shell alone would
// leave it running; the process group kill ends it. The deadline passes once the child's pid
// is written, which is then watched in /proc, so the test needs no wall clock.
func TestOSKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	ctx := newDeadlineAtLine()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sh", "-c", "sleep 30 & echo $!; sleep 30"}, StdoutTo: ctx})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("not marked timed out: %+v", res)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(ctx.written()))
	if err != nil {
		t.Fatalf("the child's pid was not printed: %q", ctx.written())
	}
	const polls = 500
	for range polls {
		if gone(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the child %d outlived the timeout", pid)
}

// A command that exits but leaves a child holding its output pipe is not a clean run: the
// capture the command did write is kept, the leftover is reported (Leftover), and the child is
// ended rather than left running.
func TestOSKeepsTheCaptureAndReportsAChildLeftHoldingThePipe(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{Argv: []string{"sh", "-c", "echo before; sleep 30 & echo $!; exit 0"}, Merge: true})
	if err != nil {
		t.Fatalf("a leftover child is a report, not a failure to run: %v", err)
	}
	if !res.Leftover || res.TimedOut || res.Code != 0 {
		t.Fatalf("want Leftover with the command's own status 0: %+v", res)
	}
	lines := strings.Fields(res.Stdout)
	if len(lines) != 2 || lines[0] != "before" {
		t.Fatalf("the capture was dropped or mangled: %q", res.Stdout)
	}
	pid, err := strconv.Atoi(lines[1])
	if err != nil {
		t.Fatalf("the child's pid was not printed: %q", res.Stdout)
	}
	const polls = 500
	for range polls {
		if gone(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the leftover child %d is still running", pid)
}

func TestOSCleanRunIsNotLeftover(t *testing.T) {
	res, err := OS{}.Run(context.Background(), Cmd{Argv: []string{"sh", "-c", "echo ok"}})
	if err != nil || res.Leftover {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestOSCancelledIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := OS{}.Run(ctx, Cmd{Argv: []string{"sleep", "30"}})
	if err == nil && res.TimedOut {
		t.Fatalf("a cancel was reported as a timeout: %+v", res)
	}
}
