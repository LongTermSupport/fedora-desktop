// Package fakeclock is a stub of a fake clock with the count API the rule forbids waiting on,
// beside the waits that name their timer.
package fakeclock

import (
	"context"
	"time"
)

// Fake moves only when a test moves it.
type Fake struct{ waiters []time.Duration }

// Advance moves the clock by d.
func (f *Fake) Advance(_ time.Duration) {}

// StepWall moves the wall clock alone: an integer, but no wait.
func (f *Fake) StepWall(_ int64) {}

// Waiters is how many waiters are armed.
func (f *Fake) Waiters() int { return len(f.waiters) }

// BlockUntil waits until n waiters are armed.
func (f *Fake) BlockUntil(_ context.Context, _ int) error { return nil }

// AwaitDue waits until a waiter due d from now is armed.
func (f *Fake) AwaitDue(_ context.Context, _ time.Duration) error { return nil }

// Count is the fake's own count, read inside the package that defines it.
func (f *Fake) Count() int {
	for f.Waiters() > 0 { // want `waits on a count of the fake clock's waiters \(Waiters\)`
		return 1
	}
	return 0
}
