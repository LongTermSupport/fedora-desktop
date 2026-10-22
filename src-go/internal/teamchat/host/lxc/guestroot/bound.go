package guestroot

import (
	"errors"
	"fmt"
	"time"
)

// ErrStalled is a guest read or write that did not end within its bound: a mount below the
// guest's root that does not answer (a FUSE mount, in a privileged container). The guest's own
// doing, so it is ErrRefused and fails that member alone (B5).
var ErrStalled = fmt.Errorf("%w: a guest call did not end within its bound", ErrRefused)

// callBound is each read's and write's longest wait.
const callBound = 10 * time.Second

// bounded runs op, a read or write below the root, between two looks at the init as live does,
// on its own goroutine, and waits at most the root's bound for it. A call not ended by then
// answers ErrStalled naming p, and is left to end by itself: whatever it then makes is given to
// late (a dialled connection is closed), and the root's descriptors, if it was closed meanwhile,
// are released when the last such call ends. Their errors join the next Close's; one that comes
// after Close is lost with the call, which was already answered.
func bounded[T any](r *Root, p string, op func() (T, error), late func(T) error) (T, error) {
	var zero T
	if err := r.Alive(); err != nil {
		return zero, err
	}
	if err := r.enter(); err != nil {
		return zero, err
	}
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := op()
		done <- result{v, errors.Join(err, r.leave())}
	}()
	timer := time.NewTimer(r.bound)
	defer timer.Stop()
	select {
	case res := <-done:
		if aerr := r.Alive(); aerr != nil {
			if res.err == nil && late != nil {
				aerr = errors.Join(aerr, late(res.v))
			}
			return zero, aerr
		}
		return res.v, res.err
	case <-timer.C:
	}
	go func() {
		if res := <-done; res.err == nil && late != nil {
			r.keepLate(late(res.v))
		}
	}()
	if aerr := r.Alive(); aerr != nil {
		return zero, aerr
	}
	return zero, fmt.Errorf("%w: %s did not answer within %v", ErrStalled, p, r.bound)
}

// enter counts a call in flight, on a root not closed.
func (r *Root) enter() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}
	r.inflight++
	return nil
}

// leave ends a call in flight; the last one to end on a closed root releases it.
func (r *Root) leave() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflight--
	if r.closed && r.inflight == 0 {
		return r.release()
	}
	return nil
}

// keepLate keeps an abandoned call's error for the next Close.
func (r *Root) keepLate(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lateErr = errors.Join(r.lateErr, err)
}
