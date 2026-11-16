package stdinreadtest

import (
	"context"
	"testing"
	"time"
)

// A hold armed is awaited and fired once its receiver takes the tick; a stopped one is never
// fired, and stop says whether it was still armed, as a timer's Stop does.
func TestAHoldIsAwaitedFiredAndStopped(t *testing.T) {
	c := NewClock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tick, stop := c.Hold(time.Second)
	if err := c.Await(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		<-tick
		close(got)
	}()
	c.Fire(time.Second)
	<-got
	if stop() {
		t.Fatal("a fired hold stopped reports true, as a fired timer's Stop does not")
	}
	_, stopArmed := c.Hold(time.Minute)
	if !stopArmed() {
		t.Fatal("an armed hold stopped reports false")
	}
	if stopArmed() {
		t.Fatal("a stopped hold stopped again reports true")
	}
	c.Fire(time.Minute)
}

// Await ends with the context when no hold of the duration is armed.
func TestAwaitEndsWithTheContext(t *testing.T) {
	c := NewClock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Await(ctx, time.Minute); err == nil {
		t.Fatal("want the context's error")
	}
}
