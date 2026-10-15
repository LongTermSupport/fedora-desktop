package execx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOSRunCancelSendsTheCommandsInterruptWhenSet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	go func() {
		buf := make([]byte, len("ready\n"))
		if _, err := io.ReadFull(pr, buf); err == nil {
			cancel()
		}
		if _, err := io.Copy(io.Discard, pr); err != nil {
			return
		}
	}()
	var out bytes.Buffer
	script := `trap 'echo interrupted; exit 0' INT; trap 'echo terminated; exit 0' TERM; echo ready; while :; do sleep 0.05; done`
	_, err := OS{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", script}, Stdout: io.MultiWriter(&out, pw), Interrupt: syscall.SIGINT})
	if cerr := pw.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if !strings.Contains(out.String(), "interrupted") || strings.Contains(out.String(), "terminated") {
		t.Fatalf("the child did not get SIGINT alone: %q", out.String())
	}
}

type cancelRun struct {
	out string
	err error
}

// runThenCancel runs script and cancels the context once the child has printed "ready", so
// the cancel never lands before the child's trap is installed.
func runThenCancel(t *testing.T, r OS, script string) cancelRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, len("ready\n"))
		if _, err := io.ReadFull(pr, buf); err == nil {
			cancel()
		}
		if _, err := io.Copy(io.Discard, pr); err != nil {
			return
		}
	}()
	var out bytes.Buffer
	finished := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", script}, Stdout: io.MultiWriter(&out, pw)})
		finished <- err
	}()
	var err error
	select {
	case err = <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel: the child was never escalated to a kill")
	}
	if cerr := pw.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	<-done
	return cancelRun{out: out.String(), err: err}
}

func TestOSRunCancelSendsSIGTERMSoTheChildCanRestoreTheTerminal(t *testing.T) {
	got := runThenCancel(t, OS{}, `trap 'echo restored; exit 0' TERM; echo ready; while :; do sleep 0.05; done`)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", got.err)
	}
	if !strings.Contains(got.out, "restored") {
		t.Fatalf("the child got no catchable signal: %q", got.out)
	}
}

func TestOSRunCancelEscalatesWhenTheChildIgnoresTERMAndHoldsThePipe(t *testing.T) {
	got := runThenCancel(t, OS{Grace: 300 * time.Millisecond}, `trap '' TERM; echo ready; sleep 30 & while :; do sleep 0.05; done`)
	if got.err == nil {
		t.Fatal("want an error from the cancelled command")
	}
}

// Grace bounds the pipe drain after a normal exit as well (exec.Cmd.WaitDelay), so the Grace
// here is far above the copy time: a Grace of a millisecond failed under -race on a loaded host.
func TestOSRunUncancelledReturnsTheWholeOutput(t *testing.T) {
	res, err := OS{Grace: 2 * time.Second}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "sleep 0.1; echo done"}})
	if err != nil || string(res.Stdout) != "done\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
}
