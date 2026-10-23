package realtime

import (
	ctxpkg "context"
	"errors"
	"time"
)

// The ways round it: an import alias, a package variable, a parameter, a deadline, every timer
// form, a Reset, a function value and a package-level initialiser.

var callBound = 10 * time.Second

var errLate = errors.New("late")

var lateTimer = time.NewTimer(callBound) // want `time.NewTimer on real time with a bound set at run time`

func aliased(ctx ctxpkg.Context) (ctxpkg.Context, ctxpkg.CancelFunc) {
	return ctxpkg.WithTimeout(ctx, callBound) // want `context.WithTimeout on real time with a bound set at run time`
}

func caused(ctx ctxpkg.Context, d time.Duration) (ctxpkg.Context, ctxpkg.CancelFunc) {
	return ctxpkg.WithTimeoutCause(ctx, d, errLate) // want `context.WithTimeoutCause on real time with a bound set at run time`
}

func deadline(ctx ctxpkg.Context) (ctxpkg.Context, ctxpkg.CancelFunc) {
	return ctxpkg.WithDeadline(ctx, time.Unix(0, 0)) // want `context.WithDeadline on real time with a bound set at run time`
}

func deadlineCause(ctx ctxpkg.Context, at time.Time) (ctxpkg.Context, ctxpkg.CancelFunc) {
	return ctxpkg.WithDeadlineCause(ctx, at, errLate) // want `context.WithDeadlineCause on real time with a bound set at run time`
}

func timers(d time.Duration, f func()) {
	<-time.After(d)              // want `time.After on real time with a bound set at run time`
	stop := time.AfterFunc(d, f) // want `time.AfterFunc on real time with a bound set at run time`
	stop.Stop()
	tick := time.NewTicker(d) // want `time.NewTicker on real time with a bound set at run time`
	tick.Reset(d)             // want `time.Ticker.Reset on real time with a bound set at run time`
	tick.Stop()
	<-time.Tick(d) // want `time.Tick on real time with a bound set at run time`
	time.Sleep(d)  // want `time.Sleep on real time with a bound set at run time`
	t := time.NewTimer(time.Second)
	t.Reset(d) // want `time.Timer.Reset on real time with a bound set at run time`
}

type seam struct {
	bound func(ctxpkg.Context, time.Duration) (ctxpkg.Context, ctxpkg.CancelFunc)
}

func valued() seam {
	return seam{bound: ctxpkg.WithTimeout} // want `context.WithTimeout taken as a value, its bound unseen`
}

func parenthesised(ctx ctxpkg.Context, d time.Duration) (ctxpkg.Context, ctxpkg.CancelFunc) {
	return (ctxpkg.WithTimeout)(ctx, d) // want `context.WithTimeout on real time with a bound set at run time`
}
