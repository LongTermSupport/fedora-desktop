// Package leakcheck is a stub of internal/leakcheck: Tests wraps a TestMain's run,
// and Check judges one test.
package leakcheck

import "testing"

// Runner is what Tests needs of testing.M.
type Runner interface{ Run() int }

// Tests is m, with a leak check after it.
func Tests(m *testing.M) Runner { return m }

// Check judges tb once its cleanups have run.
func Check(tb testing.TB) { tb.Helper() }

// Idle is whether every goroutine but the caller's is blocked.
func Idle() bool { return false }

// Poll yields until cond holds, failing tb when the world is idle and it does not.
func Poll(tb testing.TB, cond func() bool) {
	tb.Helper()
	for !cond() {
		if Idle() {
			tb.Fatal("idle")
		}
	}
}

// Drive is Poll that calls step each time the world is idle and cond does not hold.
func Drive(tb testing.TB, cond func() bool, step func()) {
	tb.Helper()
	for !cond() {
		if Idle() {
			step()
		}
	}
}
