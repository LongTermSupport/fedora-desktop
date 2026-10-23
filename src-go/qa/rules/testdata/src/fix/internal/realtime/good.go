package realtime

import (
	"context"
	"time"
)

// Clean: a bound whose length is a constant cannot be shortened by a test, and a bound on the
// injected clock is the fake's to move.

const flushWait = 2 * time.Second

type Clock interface {
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

func flush(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, flushWait)
}

func poll() {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	time.Sleep(time.Millisecond)
	<-time.After(flushWait + time.Second)
}

func onTheClock(ctx context.Context, clk Clock, bound time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := clk.AfterFunc(bound, cancel)
	return ctx, func() { stop(); cancel() }
}
