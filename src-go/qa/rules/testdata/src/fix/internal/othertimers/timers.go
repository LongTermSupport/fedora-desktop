// Package othertimers is a second fake, a value receiver behind an interface, whose count wait
// is spelled differently: the rule judges the shape, not the name.
package othertimers

import (
	"context"
	"time"
)

// Timers is a fake timer set.
type Timers struct{}

// Advance fires what falls due within d.
func (Timers) Advance(_ time.Duration) {}

// Armed is how many timers are armed.
func (Timers) Armed() int { return 0 }

// NowMS is the fake's wall clock: a reading, not a count.
func (Timers) NowMS() int64 { return 0 }

// WaitForTimers waits until n timers are armed.
func (Timers) WaitForTimers(_ context.Context, _ uint) error { return nil }

// Clock is what a test holds the fake as.
type Clock interface {
	Advance(d time.Duration)
	WaitForTimers(ctx context.Context, n uint) error
}

// Real has no Advance: a context and a count on it are not a fake clock's.
type Real struct{}

// WaitFor waits for n jobs.
func (Real) WaitFor(_ context.Context, _ int) error { return nil }

// Jobs is how many jobs run.
func (Real) Jobs() int { return 0 }
