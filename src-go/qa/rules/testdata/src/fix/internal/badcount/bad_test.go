package badcount

import (
	"context"
	"testing"
	"time"

	"fix/internal/fakeclock"
	"fix/internal/othertimers"
)

// The originating defect: a snapshot of the count, a wait for one more, then the advance. The
// server's read limit, armed on accept, made the count before the op's deadline was armed.
func TestOriginating(t *testing.T) {
	clk := &fakeclock.Fake{}
	armed := clk.Waiters()                                       // want `counts the fake clock's waiters \(Waiters\)`
	if err := clk.BlockUntil(t.Context(), armed+1); err != nil { // want `waits on a count of the fake clock's waiters \(BlockUntil\)`
		t.Fatal(err)
	}
	clk.Advance(300 * time.Millisecond)
}

// The ways round it: a method value, a helper's parameter, a polling loop, a predicate.
func TestEvasions(t *testing.T) {
	clk := &fakeclock.Fake{}
	wait := clk.BlockUntil // want `waits on a count of the fake clock's waiters \(BlockUntil\)`
	_ = wait(context.Background(), 1)
	for clk.Waiters() != 2 { // want `waits on a count of the fake clock's waiters \(Waiters\)`
	}
	for {
		if clk.Waiters() == 1 { // want `waits on a count of the fake clock's waiters \(Waiters\)`
			break
		}
	}
	until(t, func() bool { return clk.Waiters() == 1 }) // want `waits on a count of the fake clock's waiters \(Waiters\)`
	var timers othertimers.Timers
	_ = timers.WaitForTimers(t.Context(), 2) // want `waits on a count of the fake clock's waiters \(WaitForTimers\)`
	var held othertimers.Clock = timers
	_ = held.WaitForTimers(t.Context(), 2) // want `waits on a count of the fake clock's waiters \(WaitForTimers\)`
	for timers.Armed() > 0 {               // want `waits on a count of the fake clock's waiters \(Armed\)`
	}
}

// helper waits through a parameter of the fake's type.
func helper(clk *fakeclock.Fake, n int) error {
	return clk.BlockUntil(context.Background(), n) // want `waits on a count of the fake clock's waiters \(BlockUntil\)`
}

// Clean: the wait names its timer by when it is due, and so does the query; an integer with
// no context is not a count wait; nor is a count on a type that is not a fake clock.
func TestClean(t *testing.T) {
	clk := &fakeclock.Fake{}
	if err := clk.AwaitDue(t.Context(), time.Second); err != nil {
		t.Fatal(err)
	}
	if !clk.Armed(time.Second) {
		t.Fatal("not armed")
	}
	clk.Advance(time.Second)
	clk.StepWall(5)
	var timers othertimers.Timers
	until(t, func() bool { return time.UnixMilli(timers.NowMS()).IsZero() })
	var r othertimers.Real
	_ = r.WaitFor(t.Context(), 1)
	for r.Jobs() > 0 {
	}
}

func until(t *testing.T, cond func() bool) {
	t.Helper()
	for !cond() {
	}
}
