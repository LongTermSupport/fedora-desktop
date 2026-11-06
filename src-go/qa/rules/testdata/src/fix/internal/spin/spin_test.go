package spin

import (
	"runtime"
	rt "runtime"
	"sync/atomic"
	"testing"

	"fix/internal/leakcheck"
)

type world struct {
	t *testing.T
	n atomic.Int64
}

func (w *world) ready() bool { return w.n.Load() > 2 }

// until is the originating shape: it spins on the condition and has nothing else to end it.
func (w *world) until(cond func() bool) {
	for !cond() { // want `spin wait with no failure path`
		runtime.Gosched()
	}
}

// answered spins in a select's default.
func (w *world) answered(got <-chan int) int {
	for { // want `spin wait with no failure path`
		select {
		case v := <-got:
			return v
		default:
			runtime.Gosched()
		}
	}
}

// parked yields in the loop's post statement.
func (w *world) parked() {
	for tried := false; !tried; runtime.Gosched() { // want `spin wait with no failure path`
		tried = w.ready()
	}
}

// untilContext guards on the test's context, which ends only with the test: while the test
// goroutine spins the guard never fires.
func (w *world) untilContext(cond func() bool) {
	for !cond() { // want `spin wait with no failure path`
		if w.t.Context().Err() != nil {
			w.t.Fatal("the condition never held")
		}
		runtime.Gosched()
	}
}

// untilLiteral yields from a function literal the loop calls.
func (w *world) untilLiteral(cond func() bool) {
	for !cond() { // want `spin wait with no failure path`
		func() { runtime.Gosched() }()
	}
}

// untilAlias names the function through another import of the package.
func (w *world) untilAlias(cond func() bool) {
	for !cond() { // want `spin wait with no failure path`
		rt.Gosched()
	}
}

// untilValue hands the function on as a value.
func (w *world) untilValue(cond func() bool, yield func(func())) {
	for !cond() { // want `spin wait with no failure path`
		yield(runtime.Gosched)
	}
}

// untilBound ends on a count as well, which is still a wait that never fails on its own account:
// the guard is not the idle test.
func (w *world) untilBound(cond func() bool) {
	for i := 0; !cond(); i++ { // want `spin wait with no failure path`
		if i > 1_000_000 {
			w.t.Fatal("the condition never held")
		}
		runtime.Gosched()
	}
}

// nested spins in the inner loop; the outer loop does not yield itself.
func (w *world) nested(cond func() bool) {
	for range 3 {
		for !cond() { // want `spin wait with no failure path`
			runtime.Gosched()
		}
	}
}

// untilIdle fails once every other goroutine is blocked and the condition still does not hold.
func (w *world) untilIdle(cond func() bool) {
	for !cond() {
		if leakcheck.Idle() {
			w.t.Fatal("every goroutine is blocked and the condition does not hold")
		}
		runtime.Gosched()
	}
}

// untilIdleNested asks in a loop inside the spin.
func (w *world) untilIdleNested(cond func() bool) {
	for !cond() {
		for range 2 {
			if leakcheck.Idle() {
				w.t.Fatal("every goroutine is blocked and the condition does not hold")
			}
		}
		runtime.Gosched()
	}
}

// onYield is one yield, in no loop.
func (w *world) onYield() { runtime.Gosched() }

// counted yields in a range loop, which ends by itself.
func (w *world) counted() {
	for range 3 {
		runtime.Gosched()
	}
}

// noYield polls without yielding, which this rule does not judge.
func (w *world) noYield(cond func() bool) {
	for !cond() {
		w.n.Add(1)
	}
}

func TestSpin(t *testing.T) {
	w := &world{t: t}
	w.onYield()
	w.counted()
}
