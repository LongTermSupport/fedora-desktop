package clock

import (
	"context"
	"time"
)

// The owner of the real clock is not judged.

type system struct{}

func (system) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (system) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
