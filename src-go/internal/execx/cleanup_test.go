package execx

import (
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestCleanupsRunLastFirstAndOnce(t *testing.T) {
	var ran []string
	var c Cleanups
	for _, name := range []string{"key copy", "git config", "agent"} {
		c.Push(name, func() error { ran = append(ran, name); return nil })
	}
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"agent", "git config", "key copy"}; !slices.Equal(ran, want) {
		t.Fatalf("ran %q, want %q once each", ran, want)
	}
}

func TestCleanupsRunEveryStepAndReportEachFailure(t *testing.T) {
	var ran []string
	var c Cleanups
	c.Push("first", func() error { ran = append(ran, "first"); return nil })
	c.Push("second", func() error { ran = append(ran, "second"); return errors.New("busy") })
	c.Push("third", func() error { ran = append(ran, "third"); return errors.New("gone") })
	err := c.Run()
	if !slices.Equal(ran, []string{"third", "second", "first"}) {
		t.Fatalf("ran %q: a failed step stopped the rest", ran)
	}
	if err == nil || !strings.Contains(err.Error(), "clean up second: busy") || !strings.Contains(err.Error(), "clean up third: gone") {
		t.Fatalf("Run = %v, want both failures named", err)
	}
}

func TestCleanupsExecRunsThemFirst(t *testing.T) {
	f := &Fake{}
	var c Cleanups
	ran := false
	c.Push("stage", func() error {
		ran = true
		if len(f.Execs()) != 0 {
			t.Error("the process was replaced before its cleanups ran")
		}
		return nil
	})
	if err := c.Exec(f, Cmd{Name: "claude-yolo"}); err != nil {
		t.Fatal(err)
	}
	if !ran || len(f.Execs()) != 1 {
		t.Fatalf("ran=%v execs=%v", ran, f.Execs())
	}
}

func TestCleanupsExecRefusesWhenACleanupFailed(t *testing.T) {
	f := &Fake{}
	var c Cleanups
	c.Push("stage", func() error { return errors.New("busy") })
	if err := c.Exec(f, Cmd{Name: "claude-yolo"}); err == nil {
		t.Fatal("the process was replaced with a cleanup failed")
	}
	if len(f.Execs()) != 0 {
		t.Fatal("Exec ran after a failed cleanup")
	}
}

// A signal ends the context with its cause, so the flow returns, its cleanups run from the
// main's defer, and the exit code is the shell's 128+n.
func TestSignalContext(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT} {
		ctx, stop := SignalContext(context.Background())
		signalSelf(t, sig)
		<-ctx.Done()
		got, ok := errors.AsType[*SignalError](context.Cause(ctx))
		stop()
		if !ok || got.Signal != sig {
			t.Fatalf("cause = %v, want %v", context.Cause(ctx), sig)
		}
		if got.ExitCode() != 128+int(sig) || !strings.Contains(got.Error(), "signal") {
			t.Fatalf("%v: exit %d, %q", sig, got.ExitCode(), got.Error())
		}
	}
}

func TestSignalContextStop(t *testing.T) {
	ctx, stop := SignalContext(context.Background())
	stop()
	<-ctx.Done()
	if sigErr, ok := errors.AsType[*SignalError](context.Cause(ctx)); ok {
		t.Fatalf("a stopped context reports a signal: %v", sigErr)
	}
	stop()
}
