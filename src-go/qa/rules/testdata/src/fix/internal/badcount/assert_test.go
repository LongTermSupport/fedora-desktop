package badcount

import (
	"testing"

	"fix/internal/fakeclock"
	"fix/internal/othertimers"
)

// The originating assertion (the n3 merge into n3e): the one TIMEOUT timer named by a count,
// which the delivery loop's timers on the same clock made 2.
func TestAssertOriginating(t *testing.T) {
	clk := &fakeclock.Fake{}
	if clk.Waiters() != 1 { // want `counts the fake clock's waiters \(Waiters\)`
		t.Fatalf("waiters = %d, want the one TIMEOUT timer", clk.Waiters()) // want `counts the fake clock's waiters \(Waiters\)`
	}
}

// The ways round it: a count compared with zero, kept in a variable, taken as a method value,
// handed to a helper, switched on, asserted in a table loop and a subtest, on a second fake.
func TestAssertEvasions(t *testing.T) {
	clk := &fakeclock.Fake{}
	if n := clk.Waiters(); n != 0 { // want `counts the fake clock's waiters \(Waiters\)`
		t.Fatal(n)
	}
	count := clk.Waiters // want `counts the fake clock's waiters \(Waiters\)`
	expect(t, count(), 1)
	expect(t, clk.Waiters(), 0) // want `counts the fake clock's waiters \(Waiters\)`
	switch clk.Waiters() {      // want `counts the fake clock's waiters \(Waiters\)`
	case 0:
	}
	for _, want := range []int{0} {
		if clk.Waiters() != want { // want `counts the fake clock's waiters \(Waiters\)`
			t.Fatal(want)
		}
	}
	t.Run("sub", func(t *testing.T) {
		if clk.Waiters() != 0 { // want `counts the fake clock's waiters \(Waiters\)`
			t.Fatal("armed")
		}
	})
	var timers othertimers.Timers
	if timers.Armed() == 0 { // want `counts the fake clock's waiters \(Armed\)`
		t.Fatal("none")
	}
}

// armedCount hands the count on from a helper outside the fake's package.
func armedCount(clk *fakeclock.Fake) int {
	return clk.Waiters() // want `counts the fake clock's waiters \(Waiters\)`
}

func expect(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatal(got)
	}
}
