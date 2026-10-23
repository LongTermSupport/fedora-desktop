package keeper

import (
	"syscall"
	"testing"
)

// The fake child and pidfd kill before they announce a SIGKILL, so a test that has read the
// signal finds the exit done, as a real SIGKILL's is. The signal channel is unbuffered here, so
// the read meets the send itself.
func TestAFakeSIGKILLExitsBeforeItIsSeen(t *testing.T) {
	c := &child{sigs: make(chan syscall.Signal), done: make(chan struct{})}
	p := &fakePidfd{sigs: make(chan syscall.Signal), exited: make(chan struct{})}
	errs := make(chan error, 2)
	go func() { errs <- c.Signal(syscall.SIGKILL) }()
	go func() { errs <- p.Signal(syscall.SIGKILL) }()
	if s := c.signal(); s != syscall.SIGKILL {
		t.Fatalf("child sent %v", s)
	}
	select {
	case <-c.done:
	default:
		t.Fatal("the child's SIGKILL was seen before it exited")
	}
	if s := <-p.sigs; s != syscall.SIGKILL {
		t.Fatalf("pidfd sent %v", s)
	}
	select {
	case <-p.exited:
	default:
		t.Fatal("the pidfd's SIGKILL was seen before it exited")
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
