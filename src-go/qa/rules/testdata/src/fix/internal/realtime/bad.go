package realtime

import (
	"context"
	"time"
)

// The originating shapes: the L3 probe's bound from a field a test shortens, the guest root's
// timer on its field, and a call bound from a struct field.

type Guests struct{ Bound time.Duration }

func (g Guests) bound() time.Duration {
	if g.Bound > 0 {
		return g.Bound
	}
	return 5 * time.Second
}

func (g Guests) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, g.bound()) // want `context.WithTimeout on real time with a bound set at run time`
	defer cancel()
	<-ctx.Done()
	return ctx.Err()
}

type root struct{ bound time.Duration }

func (r *root) bounded(done <-chan struct{}) bool {
	timer := time.NewTimer(r.bound) // want `time.NewTimer on real time with a bound set at run time`
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

type systemd struct{ wait time.Duration }

func (s systemd) call(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.wait) // want `context.WithTimeout on real time with a bound set at run time`
	defer cancel()
	return ctx.Err()
}
