package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A real timer as the passing case of a select: the originating link test's "no cancel came
// within 100 ms", the same through a Timer's C and an assignment, and a body that does
// something but never fails.
func TestNothingCameInTime(t *testing.T) {
	cancelled := make(chan struct{})
	select {
	case <-cancelled:
		t.Fatal("the cancel came first")
	case <-time.After(100 * time.Millisecond): // want `a real timer is the passing case of a select`
	}
	timer := time.NewTimer(time.Second)
	select {
	case <-cancelled:
		t.Fatal("cancelled")
	case at := <-timer.C: // want `a real timer is the passing case of a select`
		t.Log(at)
	}
	select {
	case <-cancelled:
	case <-(time.After(time.Second)): // want `a real timer is the passing case of a select`
		go func() { t.Error("in a goroutine, which does not fail this case") }()
	}
}

// A ticker or time.Tick as the passing case races as a timer does; in a loop, a timer case that
// does more than go round is still the passing case.
func TestNothingCameByTheTick(t *testing.T) {
	cancelled := make(chan struct{})
	tick := time.NewTicker(90 * time.Millisecond)
	defer tick.Stop()
	select {
	case <-cancelled:
		t.Fatal("cancelled")
	case <-tick.C: // want `a real timer is the passing case of a select`
	}
	select {
	case <-cancelled:
		t.Fatal("cancelled")
	case <-time.Tick(time.Second): // want `a real timer is the passing case of a select`
	}
	for {
		select {
		case <-cancelled:
			t.Fatal("cancelled")
		case <-time.After(time.Second): // want `a real timer is the passing case of a select`
			t.Log("nothing came")
			return
		}
	}
}

// A real timer that acts on its own stands in for the step: the freeze tools' "Ctrl-C at the
// prompt" cancelled after 50 ms, hoping the prompt had been reached; called, aliased or as a
// value.
func TestCancelledAfterADelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel) // want `time.AfterFunc in a test acts on real time`
	later := time.AfterFunc                     // want `time.AfterFunc in a test acts on real time`
	later(time.Second, cancel).Stop()
	<-ctx.Done()
}

func within(done <-chan struct{}) error {
	select {
	case <-done:
		return errors.New("done")
	case <-time.After(time.Second): // want `a real timer is the passing case of a select`
		return nil
	}
}

// Clean: a real timer that fails, as a safety net; a return of an error handed up; a panic;
// a ticker's poll gap; a fake clock's channel; a real timer outside a select is the
// production rule's, which test files are not judged by.
func TestSafetyNets(t *testing.T) {
	done := make(chan struct{})
	close(done)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("never done")
	}
	bound := time.NewTimer(time.Second)
	defer bound.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		case <-bound.C:
			t.Fatalf("not within a second")
		}
	}
}

// A counted loop, or a loop that goes on after its select, passes once the timer has fired
// its count: the timer case is the passing case still.
func TestCountedPolls(t *testing.T) {
	done := make(chan struct{})
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for range 3 {
		select {
		case <-tick.C: // want `a real timer is the passing case of a select`
		case <-done:
			t.Fatal("done")
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			return
		case <-time.After(time.Millisecond): // want `a real timer is the passing case of a select`
			continue
		}
	}
	for {
		select {
		case <-done:
			t.Fatal("done")
		case <-time.After(time.Millisecond): // want `a real timer is the passing case of a select`
		}
		break
	}
}

func TestMoreSafetyNets(t *testing.T) {
	done := make(chan struct{})
	close(done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Minute):
		panic("never")
	}
	fake := make(chan time.Time)
	select {
	case <-done:
	case <-fake:
	}
}

func probe(done <-chan struct{}) (int, error) {
	select {
	case <-done:
		return 1, nil
	case <-time.After(10 * time.Second):
		return 0, errors.New("the probes ran one at a time")
	}
}
