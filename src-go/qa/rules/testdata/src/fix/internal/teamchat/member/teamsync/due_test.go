package teamsync

import (
	"testing"
	"time"

	"fix/internal/fakeclock"
)

// reportsSettled stands for the helper that waits for the reports room's state and for the try
// that put it there to have ended, so the join's bound is no longer armed.
func reportsSettled(t *testing.T) { t.Helper() }

// The originating defect (TestARestoreUnderALongPoll): the due is named while the reports join,
// bounded by the same 30 s, may still be in flight, so AwaitDue is answered by the join's bound
// and the advance never reaches the long-poll's hold.
func TestRestoreUnderALongPoll(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), longPoll) // want `project_shared_due`
	clk.Advance(longPoll)
}

// The same 30 s spelt otherwise, and the bool query.
func TestSpelledOtherwise(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), 30*time.Second) // want `project_shared_due`
	_ = clk.Armed(reportsJoinLimit)               // want `project_shared_due`
	_ = clk.Armed(60 * time.Second / 2)           // want `project_shared_due`
}

// A subtest's closure is part of the test that holds it.
func TestInAClosure(t *testing.T) {
	t.Run("restore", func(t *testing.T) {
		clk := &fakeclock.Fake{}
		_ = clk.AwaitDue(t.Context(), longPoll) // want `project_shared_due`
	})
}

// A helper that names the due is judged on its own; the test that calls it settles first.
func waitForTheHold(t *testing.T, clk *fakeclock.Fake) {
	_ = clk.AwaitDue(t.Context(), longPoll) // want `project_shared_due`
}

// Settled first: the join's bound is stopped, so the due is the long-poll's alone.
func TestSettled(t *testing.T) {
	reportsSettled(t)
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), longPoll)
	clk.Advance(longPoll)
}

// Settled in the test and asked in its closure.
func TestSettledThenAClosure(t *testing.T) {
	reportsSettled(t)
	t.Run("hold", func(t *testing.T) {
		clk := &fakeclock.Fake{}
		_ = clk.Armed(longPoll)
	})
}

// A due the join's bound does not share.
func TestAnotherDue(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), otherWait)
	_ = clk.Armed(time.Second)
}

// Moving the clock names no due: an Advance alone is not judged.
func TestAdvanceAlone(t *testing.T) {
	clk := &fakeclock.Fake{}
	clk.Advance(longPoll)
}

// A duration that is not a constant cannot be judged.
func TestComputedDue(t *testing.T) {
	clk := &fakeclock.Fake{}
	d := time.Duration(len(t.Name())) * time.Second
	_ = clk.AwaitDue(t.Context(), d)
}
