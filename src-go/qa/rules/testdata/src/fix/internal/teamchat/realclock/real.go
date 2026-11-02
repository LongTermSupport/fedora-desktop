package realclock

import (
	"context"
	"time"
)

// A realClocks row excuses the real side of a clock seam, and nothing beside it.

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

func neighbour(d time.Duration) {
	time.Sleep(d) // want `time.Sleep on real time with a bound set at run time`
}
