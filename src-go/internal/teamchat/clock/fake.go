package clock

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Fake is a clock that moves only when a test moves it. Its waiters are due on its monotonic
// reading, as the runtime's timers are: Advance fires what falls due, in due order (ties in the
// order armed), each with the clock standing at its own due time, on the caller's goroutine;
// Suspend fires nothing.
type Fake struct {
	mu      sync.Mutex
	boot    time.Duration
	mono    time.Duration
	wall    int64
	seq     uint64
	waiters []*waiter
	// changed is closed and replaced whenever the waiters change, for BlockUntil.
	changed chan struct{}
}

type waiter struct {
	due  time.Duration
	seq  uint64
	fire func()
}

// NewFake is a fake standing at boot (boot and mono alike) and wallMS.
func NewFake(boot time.Duration, wallMS int64) *Fake {
	return &Fake{boot: boot, mono: boot, wall: wallMS, changed: make(chan struct{})}
}

// Boot is the fake's boot-time reading.
func (f *Fake) Boot() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boot
}

// Mono is the fake's monotonic reading.
func (f *Fake) Mono() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mono
}

// WallMS is the fake's wall clock.
func (f *Fake) WallMS() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wall
}

// Sleep waits until the fake has advanced d, or returns the context's error.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	woke := make(chan struct{})
	stop := f.AfterFunc(d, func() { close(woke) })
	select {
	case <-woke:
		return nil
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
}

// AfterFunc calls fn once the fake has advanced d.
func (f *Fake) AfterFunc(d time.Duration, fn func()) func() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	w := &waiter{due: f.mono + d, seq: f.seq, fire: fn}
	f.waiters = append(f.waiters, w)
	f.signal()
	return func() bool { return f.disarm(w) }
}

// Hold is a channel that receives the fake's wall time once it has advanced d.
func (f *Fake) Hold(d time.Duration) (<-chan time.Time, func() bool) {
	c := make(chan time.Time, 1)
	stop := f.AfterFunc(d, func() { c <- time.UnixMilli(f.WallMS()) })
	return c, stop
}

// Waiters is how many sleeps, timers and holds are armed.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiters)
}

// BlockUntil returns once exactly n waiters are armed, or with the context's error: a test
// waits on that before it advances, never on time.
func (f *Fake) BlockUntil(ctx context.Context, n int) error {
	for {
		f.mu.Lock()
		count, changed := len(f.waiters), f.changed
		f.mu.Unlock()
		if count == n {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Advance moves boot, mono and wall by d, firing each waiter that falls due on the way.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	end := f.mono + d
	f.mu.Unlock()
	for {
		f.mu.Lock()
		w := f.next(end)
		if w == nil {
			f.move(end - f.mono)
			f.mu.Unlock()
			return
		}
		f.move(w.due - f.mono)
		f.remove(w)
		f.mu.Unlock()
		w.fire()
	}
}

// Suspend moves boot and wall by d but not mono, as a suspended machine does; nothing fires.
func (f *Fake) Suspend(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boot += d
	f.wall += d.Milliseconds()
}

// StepWall moves the wall clock alone by ms, as a clock step does.
func (f *Fake) StepWall(ms int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall += ms
}

// move advances every reading by d; the caller holds mu.
func (f *Fake) move(d time.Duration) {
	f.boot += d
	f.mono += d
	f.wall += d.Milliseconds()
}

// next is the earliest waiter due by end, nil when none is; the caller holds mu.
func (f *Fake) next(end time.Duration) *waiter {
	var first *waiter
	for _, w := range f.waiters {
		if w.due <= end && (first == nil || w.due < first.due || w.due == first.due && w.seq < first.seq) {
			first = w
		}
	}
	return first
}

func (f *Fake) disarm(w *waiter) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remove(w)
}

// remove takes w from the armed waiters, false when it was not armed; the caller holds mu.
func (f *Fake) remove(w *waiter) bool {
	i := slices.Index(f.waiters, w)
	if i < 0 {
		return false
	}
	f.waiters = slices.Delete(f.waiters, i, i+1)
	f.signal()
	return true
}

func (f *Fake) signal() {
	close(f.changed)
	f.changed = make(chan struct{})
}
