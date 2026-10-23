package realclock

import (
	"context"
	"time"
)

// A realClocks row excuses the real side of a clock seam, and a processBounds row a bound on a
// real child process; neither excuses anything beside it.

type systemClock struct{}

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runChild(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, limit)
}

func neighbour(d time.Duration) {
	time.Sleep(d) // want `time.Sleep on real time with a bound set at run time`
}

func (systemClock) After(d time.Duration) <-chan time.Time {
	return time.After(d) // want `time.After on real time with a bound set at run time`
}
