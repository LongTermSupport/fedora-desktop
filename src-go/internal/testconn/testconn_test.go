package testconn

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recorder is the part of a test Bound reports to.
type recorder struct {
	testing.TB

	mu       sync.Mutex
	errs     []string
	cleanups []func()
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, _ ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, format)
}

func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

func (r *recorder) cleanUp() {
	for _, f := range slices.Backward(r.cleanups) {
		f()
	}
}

// closer counts its closes and answers each with err.
type closer struct {
	mu     sync.Mutex
	closes int
	err    error
}

func (c *closer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
	return c.err
}

// manual is a timer seam that fires when the test says.
type manual struct {
	fire    func()
	stopped bool
}

func (m *manual) after(f func()) func() bool {
	m.fire = f
	return func() bool {
		wasArmed := !m.stopped && m.fire != nil
		m.stopped = true
		return wasArmed
	}
}

func TestAWaitPastTheLimitFailsTheTestAndClosesTheConnection(t *testing.T) {
	rec, c, clock := &recorder{TB: t}, &closer{}, &manual{}
	bound(rec, c, clock.after)
	clock.fire()
	rec.cleanUp()
	if c.closes != 1 || len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "no answer within") {
		t.Fatalf("closes %d, errors %q", c.closes, rec.errs)
	}
}

func TestAWaitThatEndsInTimeChangesNothing(t *testing.T) {
	rec, c, clock := &recorder{TB: t}, &closer{}, &manual{}
	bound(rec, c, clock.after)
	rec.cleanUp()
	if c.closes != 0 || len(rec.errs) != 0 || !clock.stopped {
		t.Fatalf("closes %d, errors %q, stopped %v", c.closes, rec.errs, clock.stopped)
	}
}

func TestAFailedCloseIsReportedToo(t *testing.T) {
	rec, c, clock := &recorder{TB: t}, &closer{err: errors.New("refused")}, &manual{}
	bound(rec, c, clock.after)
	clock.fire()
	rec.cleanUp()
	if len(rec.errs) != 2 {
		t.Fatalf("errors %q, want the timeout and the close", rec.errs)
	}
}

// The real timer: a bound stopped by the test's end never fires.
func TestBoundUsesARealTimerTheCleanupStops(t *testing.T) {
	c := &closer{}
	t.Run("bounded", func(t *testing.T) { Bound(t, c) })
	if c.closes != 0 {
		t.Fatalf("closed %d times by a bound the test's end stopped", c.closes)
	}
}
