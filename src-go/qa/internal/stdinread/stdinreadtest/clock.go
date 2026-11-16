// Package stdinreadtest is a clock for tests of a read through qa/internal/stdinread: a hold
// fires only when the test fires it, and a fire waits until the reader has taken the tick, so a
// notice is said before the next tick is fired.
package stdinreadtest

import (
	"context"
	"sync"
	"time"
)

// Clock is the test clock (stdinread.Clock).
type Clock struct {
	mu      sync.Mutex
	held    map[*hold]time.Duration
	changed chan struct{}
}

// hold is one armed Hold: its tick, and stopped, closed by its stop.
type hold struct {
	tick    chan time.Time
	stopped chan struct{}
	once    sync.Once
}

// NewClock is a clock with nothing held.
func NewClock() *Clock {
	return &Clock{held: map[*hold]time.Duration{}, changed: make(chan struct{})}
}

// Hold implements stdinread.Clock: a tick Fire(d) sends, and its stop, which reports whether
// the hold was still armed.
func (c *Clock) Hold(d time.Duration) (<-chan time.Time, func() bool) {
	h := &hold{tick: make(chan time.Time), stopped: make(chan struct{})}
	c.mu.Lock()
	c.held[h] = d
	c.signalLocked()
	c.mu.Unlock()
	return h.tick, func() bool {
		c.mu.Lock()
		_, armed := c.held[h]
		delete(c.held, h)
		c.mu.Unlock()
		h.once.Do(func() { close(h.stopped) })
		return armed
	}
}

// Await returns once a hold of d is armed, or with ctx's error.
func (c *Clock) Await(ctx context.Context, d time.Duration) error {
	for {
		c.mu.Lock()
		armed := false
		for _, held := range c.held {
			armed = armed || held == d
		}
		changed := c.changed
		c.mu.Unlock()
		if armed {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Fire sends the tick of every hold of d armed now, each once its reader takes it or it is
// stopped; a fired hold is no longer armed.
func (c *Clock) Fire(d time.Duration) {
	c.mu.Lock()
	var due []*hold
	for h, held := range c.held {
		if held == d {
			due = append(due, h)
			delete(c.held, h)
		}
	}
	c.signalLocked()
	c.mu.Unlock()
	for _, h := range due {
		select {
		case h.tick <- time.Time{}:
		case <-h.stopped:
		}
	}
}

// signalLocked wakes every Await; c.mu is held.
func (c *Clock) signalLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}
