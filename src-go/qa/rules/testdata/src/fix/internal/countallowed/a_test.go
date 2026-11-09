package countallowed

import (
	"testing"

	"fix/internal/fakeclock"
)

// Clean: a row of the allowance table names this test, whose claim is that nothing waits.
func TestNothingWaits(t *testing.T) {
	clk := &fakeclock.Fake{}
	if clk.Waiters() != 0 {
		t.Fatalf("%d waiters", clk.Waiters())
	}
	// A wait by count is never excused.
	for clk.Waiters() != 0 { // want `waits on a count of the fake clock's waiters \(Waiters\)`
	}
}

// The neighbour has no row.
func TestNeighbour(t *testing.T) {
	clk := &fakeclock.Fake{}
	if clk.Waiters() != 0 { // want `counts the fake clock's waiters \(Waiters\)`
		t.Fatal("armed")
	}
}
