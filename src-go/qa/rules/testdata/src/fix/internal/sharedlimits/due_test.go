package sharedlimits

import (
	"testing"
	"time"

	"fix/internal/fakeclock"
)

// testOnly shares IdleLimit's value but is the test's own: only the package's limits count.
const testOnly = 2 * time.Minute

// The originating defect (TestT10ASilentClientIsClosedAtRequestLimit): the request's bound is
// named by its due while the handshake's bound, due the same 10 s from the same instant, may be
// the one armed, so the advance lands between the two and the request's is armed after it.
func TestSilentClient(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), RequestLimit) // want `project_shared_due`
	clk.Advance(RequestLimit)
}

// The same due spelt otherwise, the bool query, and in a closure.
func TestSpelledOtherwise(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), 10*time.Second) // want `project_shared_due`
	_ = clk.Armed(HandshakeLimit)                 // want `project_shared_due`
	t.Run("closure", func(t *testing.T) {
		_ = clk.AwaitDue(t.Context(), 20*time.Second/2) // want `project_shared_due`
	})
}

// A helper is judged on its own.
func waitForTheRequest(t *testing.T, clk *fakeclock.Fake) {
	_ = clk.AwaitDue(t.Context(), RequestLimit) // want `project_shared_due`
}

// Clean: a due one limit alone has, a test-only constant beside it, a timer named by the
// function that arms it, a computed due and an Advance alone.
func TestClean(t *testing.T) {
	clk := &fakeclock.Fake{}
	_ = clk.AwaitDue(t.Context(), IdleLimit)
	_ = clk.AwaitDue(t.Context(), testOnly)
	_ = clk.AwaitDueFrom(t.Context(), RequestLimit, "firstRequest")
	d := time.Duration(len(t.Name())) * time.Second
	_ = clk.AwaitDue(t.Context(), d)
	clk.Advance(RequestLimit)
}
