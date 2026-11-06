package pollpure

import (
	"sync/atomic"
	"testing"
	"time"

	"fix/internal/leakcheck"
)

type world struct {
	t   *testing.T
	clk *Fake
	n   atomic.Int64
}

func (w *world) ready() bool { return w.n.Load() > 2 }

// advancing is the originating shape: the condition moves the clock when it is false, so the
// second read Poll makes after it saw every goroutine blocked moves the world it judged idle.
func (w *world) advancing(got <-chan int) {
	var v int
	leakcheck.Poll(w.t, func() bool {
		select {
		case v = <-got:
			return true
		default:
			w.clk.Advance(time.Second) // want `moves a fake clock`
			return false
		}
	})
	_ = v
}

func (w *world) suspends() {
	leakcheck.Poll(w.t, func() bool {
		w.clk.Suspend(time.Second) // want `moves a fake clock`
		return w.ready()
	})
}

func (w *world) steps() {
	leakcheck.Poll(w.t, func() bool {
		w.clk.StepWall(1) // want `moves a fake clock`
		return w.ready()
	})
}

func (w *world) sends(out chan<- int) {
	leakcheck.Poll(w.t, func() bool {
		out <- 1 // want `sends on a channel`
		return w.ready()
	})
}

func (w *world) sendsInASelect(out chan<- int) {
	leakcheck.Poll(w.t, func() bool {
		select {
		case out <- 1: // want `sends on a channel`
		default:
		}
		return w.ready()
	})
}

func (w *world) closes(done chan struct{}) {
	leakcheck.Poll(w.t, func() bool {
		close(done) // want `closes a channel`
		return w.ready()
	})
}

// nested acts inside a function literal of the condition: it is the condition's code.
func (w *world) nested() {
	leakcheck.Poll(w.t, func() bool {
		step := func() { w.clk.Advance(time.Second) } // want `moves a fake clock`
		step()
		return w.ready()
	})
}

// driveCondition: Drive's condition is as pure as Poll's, and its step is where the world is acted on.
func (w *world) driveCondition() {
	leakcheck.Drive(w.t, func() bool {
		w.clk.Advance(time.Second) // want `moves a fake clock`
		return w.ready()
	}, func() {})
}

// driveStep is the clean form: the condition reads and the step acts.
func (w *world) driveStep() {
	leakcheck.Drive(w.t, w.ready, func() { w.clk.Advance(time.Second) })
}

// pure reads the world and nothing else.
func (w *world) pure() {
	leakcheck.Poll(w.t, func() bool { return w.ready() })
	leakcheck.Poll(w.t, func() bool { return w.clk.Waiters() == 2 })
	leakcheck.Poll(w.t, w.ready)
}

// receives takes a value: the condition then ends the wait, and a receive that finds nothing
// changes nothing.
func (w *world) receives(got <-chan error) {
	leakcheck.Poll(w.t, func() bool {
		select {
		case err := <-got:
			w.t.Fatalf("ended: %v", err)
		default:
		}
		return w.ready()
	})
}

// elsewhere moves the clock outside any condition.
func (w *world) elsewhere() {
	w.clk.Advance(time.Second)
	leakcheck.Poll(w.t, w.ready)
}

// another type's Advance is not a fake clock's.
func (w *world) another(o Other) {
	leakcheck.Poll(w.t, func() bool {
		o.Advance(time.Second)
		return w.ready()
	})
}

// aVariable hands on a condition the rule cannot see into: not caught.
func (w *world) aVariable() {
	cond := func() bool {
		w.clk.Advance(time.Second)
		return w.ready()
	}
	leakcheck.Poll(w.t, cond)
}

func TestUses(t *testing.T) {
	w := &world{t: t, clk: &Fake{}}
	w.pure()
	w.driveStep()
}
