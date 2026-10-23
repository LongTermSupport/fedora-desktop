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
	select {
	case <-tick.C:
	case <-bound.C:
		t.Fatalf("not within a second")
	}
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
