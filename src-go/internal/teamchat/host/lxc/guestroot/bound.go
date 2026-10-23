package guestroot

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"
	"time"
)

// ErrStalled is a guest read or write that did not end within its bound: a mount below the
// guest's root that does not answer (a FUSE mount, in a privileged container). The guest's own
// doing, so it is ErrGuest and fails that member alone (B5); it is never a TB1 refusal, which
// a caller may read as a free or empty answer.
var ErrStalled = fmt.Errorf("%w: a guest call did not end within its bound", ErrGuest)

// Memo is one member's stalls over one run (R2-3): once a call through a Root scoped to it is
// abandoned at its bound, every later call through it answers ErrStalled at once, naming that
// first stall, so a mount that does not answer is waited out once and not once per call.
type Memo struct {
	mu      sync.Mutex
	stalled error
}

// Scoped is a view of the same root whose calls share m's stalls; its Close closes the root.
func (r *Root) Scoped(m *Memo) *Root { return &Root{pinned: r.pinned, memo: m} }

// prior is the memo's first stall, nil with none or with no memo.
func (m *Memo) prior() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stalled
}

// keep records err as the memo's stall, when it is the first.
func (m *Memo) keep(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stalled == nil {
		m.stalled = err
	}
}

// callBound is each read's and write's longest wait.
const callBound = 10 * time.Second

// bounded runs op, a read or write below the root, between two looks at the init as live does,
// on its own goroutine, and waits at most the root's bound for it. A call not ended by then
// answers ErrStalled naming p, and is left to end by itself: whatever it then makes it gives to
// late (a dialled connection is closed), and the root's descriptors, if it was closed meanwhile,
// are released when the last such call ends. Their errors join the next Close's; one that comes
// after Close is lost with the call, which was already answered.
func bounded[T any](r *Root, p string, op func() (T, error), late func(T) error) (T, error) {
	var zero T
	if err := r.Alive(); err != nil {
		return zero, err
	}
	if prior := r.memo.prior(); prior != nil {
		return zero, fmt.Errorf("%s was not tried: %w", p, prior)
	}
	if err := r.enter(); err != nil {
		return zero, err
	}
	type result struct {
		v   T
		err error
	}
	// The call and its caller meet under mu: a call that ends after its caller gave up hands
	// what it made to late itself, so no second goroutine waits on it.
	var (
		mu               sync.Mutex
		abandoned, ended bool
	)
	done := make(chan result, 1)
	go func() {
		v, err := op()
		res := result{v, errors.Join(guestFault(err), r.leave())}
		mu.Lock()
		gone := abandoned
		ended = true
		mu.Unlock()
		if gone && res.err == nil && late != nil {
			r.keepLate(late(res.v))
		}
		done <- res
	}()
	answer := func(res result) (T, error) {
		if aerr := r.Alive(); aerr != nil {
			if res.err == nil && late != nil {
				aerr = errors.Join(aerr, late(res.v))
			}
			return zero, aerr
		}
		return res.v, res.err
	}
	timer := time.NewTimer(r.bound)
	defer timer.Stop()
	select {
	case res := <-done:
		return answer(res)
	case <-timer.C:
	}
	mu.Lock()
	inTime := ended
	abandoned = !ended
	mu.Unlock()
	if inTime {
		return answer(<-done)
	}
	if aerr := r.Alive(); aerr != nil {
		return zero, aerr
	}
	stall := fmt.Errorf("%w: %s did not answer within %v", ErrStalled, p, r.bound)
	r.memo.keep(stall)
	return zero, stall
}

// guestFault is an error of a call below the root classed by its origin, not its errno (R2-1,
// C1): ErrGuest, unless it is already classed (a refusal, a cap, a path not there) or it is a
// connect the guest's socket did not answer, which its caller reads as the bus down. No errno is
// the host's here: a guest FUSE server chooses every errno it answers, EMFILE, ENFILE and ENOMEM
// too, so a real host exhaustion fails each member alone.
func guestFault(err error) error {
	switch {
	case err == nil, errors.Is(err, ErrRefused), errors.Is(err, ErrTooLarge), errors.Is(err, ErrGuest),
		errors.Is(err, fs.ErrNotExist), errors.Is(err, errClosed):
		return err
	}
	if oe, ok := errors.AsType[*net.OpError](err); ok && oe != nil {
		return err
	}
	return fmt.Errorf("%w: %w", ErrGuest, err)
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
