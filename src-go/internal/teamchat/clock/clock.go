// Package clock is the member's clocks (Plan 00209 SPEC-n3 CK1): CLOCK_BOOTTIME for every
// deadline, backoff and request deadline_ms (B 2.4.4), CLOCK_MONOTONIC only to read a suspend
// as boot minus mono (SY12), and the wall clock only for what is compared with a server's
// origin_server_ts. Its waits (Sleep, AfterFunc, Hold) run on the runtime's timers, which do
// not advance while the machine is suspended.
//
// Clock meets lock.Clock, matrix.Clock, matrix.Timers and fakehs's Hold structurally, so this
// package imports none of them. Fake is the clock every test drives: it moves only when told,
// and a test waits on its armed waiters (BlockUntil), never on time.
package clock

import (
	"context"
	"time"

	"golang.org/x/sys/unix"
)

// Clock is what the keeper, the link and their clients read and wait on.
type Clock interface {
	// Boot is CLOCK_BOOTTIME since boot: deadlines, backoff and deadline_ms (LN2).
	Boot() time.Duration
	// Mono is CLOCK_MONOTONIC: only for SY12's boot-minus-mono reading.
	Mono() time.Duration
	// WallMS is the wall clock in milliseconds: anything compared with origin_server_ts.
	WallMS() int64
	// Sleep waits d, or returns the context's error when it ends first.
	Sleep(ctx context.Context, d time.Duration) error
	// AfterFunc calls f once after d; stop disarms it, true when it had not fired.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
	// Hold is a channel that receives once after d, and its stop, as time.NewTimer's.
	Hold(d time.Duration) (<-chan time.Time, func() bool)
}

// System is the machine's clock.
func System() Clock { return system{} }

type system struct{}

func (system) Boot() time.Duration { return read(unix.CLOCK_BOOTTIME) }

func (system) Mono() time.Duration { return read(unix.CLOCK_MONOTONIC) }

func (system) WallMS() int64 { return time.Now().UnixMilli() }

// read is one clock's reading; clock_gettime of these clocks cannot fail on Linux, so a
// failure is a broken kernel and panics rather than reading as time zero.
func read(id int32) time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(id, &ts); err != nil {
		panic("clock: clock_gettime: " + err.Error())
	}
	return time.Duration(ts.Nano())
}

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

func (system) Hold(d time.Duration) (<-chan time.Time, func() bool) {
	t := time.NewTimer(d)
	return t.C, t.Stop
}
