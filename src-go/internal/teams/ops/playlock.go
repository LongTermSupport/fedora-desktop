package ops

import (
	"context"
	"errors"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// DefaultPlayLockWait bounds how long a flow waits for another play run to let the play lock go.
const DefaultPlayLockWait = 30 * time.Minute

// errStoppedWaiting is the person choosing Stop waiting on the wait screen.
var errStoppedWaiting = errors.New("the person stopped waiting")

// errPlayOutsideLock is a play about to run with no hold to hand it: a flow that skipped Locked.
var errPlayOutsideLock = errors.New("refusing to run a play outside the play lock")

// lockNotTakenError is a flow that did not get the play lock, having said why; cancelled is the
// person stopping the wait (the screen, or Ctrl-C), not the bound or a broken lock.
type lockNotTakenError struct {
	cancelled bool
	cause     error
}

func (e *lockNotTakenError) Error() string { return "the play lock was not taken: " + e.cause.Error() }

func (e *lockNotTakenError) Unwrap() []error {
	if e.cancelled {
		return []error{playlock.ErrNotTaken, playlock.ErrStopped, e.cause}
	}
	return []error{playlock.ErrNotTaken, e.cause}
}

// LockExit is the exit of a flow that did not get the play lock: cancelled when the person
// stopped the wait, else a failure.
func LockExit(err error) term.ExitCode {
	if errors.Is(err, playlock.ErrStopped) {
		return term.ExitCancelled
	}
	return term.ExitFailure
}

// Locked runs fn holding the host's play lock, taken first (waiting for another play run if
// one holds it, see takePlayLock), so that what fn writes to the host_vars and the plays it
// runs happen in one hold: a person who stops the wait, or a wait that reaches its bound,
// leaves nothing changed. purpose says what the hold is for ("apply alpha"), in the holder
// line another run is refused with and in what the wait says. A Locked inside another is the
// same hold. A lock not taken is an error wrapping playlock.ErrNotTaken, already said.
func (o *Ops) Locked(ctx context.Context, purpose string, fn func(context.Context) error) error {
	if o.held != nil {
		return fn(ctx)
	}
	lock, err := o.takePlayLock(ctx, purpose)
	if err != nil {
		return err
	}
	o.held = lock
	err = fn(ctx)
	o.held = nil
	if relErr := lock.Release(); relErr != nil {
		o.Term.PrintError(relErr.Error())
		return errors.Join(err, relErr)
	}
	return err
}

// takePlayLock takes the play lock at once when it is free. When another run holds it, it says
// who (what it is, its pid, how long it has held it) and waits for it, up to PlayLockWait: on a
// terminal on a screen whose Stop waiting row ends the wait, otherwise saying so on stderr,
// where Ctrl-C ends it. The wait ends only in the lock taken on this descriptor.
func (o *Ops) takePlayLock(ctx context.Context, purpose string) (*playlock.Lock, error) {
	what := "ccy-teams: " + purpose
	lock, err := o.PlayLock.Take(ctx, what, 0, nil)
	if err == nil {
		return lock, nil
	}
	busy, ok := errors.AsType[*playlock.TimeoutError](err)
	if !ok {
		o.Term.PrintError("the play lock cannot be used: " + err.Error() + ".")
		return nil, &lockNotTakenError{cause: err}
	}
	w := lockWait{purpose: purpose, what: what, began: o.PlayLock.Now()}
	w.deadline = w.began.Add(o.PlayLockWait)
	if o.interactive() && o.hasTool("fzf") {
		lock, err = o.waitOnScreen(ctx, w, busy.Holder)
	} else {
		lock, err = o.waitSaying(ctx, w, busy.Holder)
	}
	return o.waitEnded(ctx, w, lock, err)
}

// lockWait is one wait for the play lock: what for, the holder line it writes once it holds
// it, and when it began and gives up.
type lockWait struct {
	purpose, what   string
	began, deadline time.Time
}

// until is the wait's bound for a person: how long, and the time it ends.
func (o *Ops) until(w lockWait) string {
	return "up to " + playlock.Duration(o.PlayLockWait) + ", until " + w.deadline.UTC().Format("15:04:05") + " UTC"
}

// waitEnded says how the wait ended, and is the lock, or the error a flow stops on.
func (o *Ops) waitEnded(ctx context.Context, w lockWait, lock *playlock.Lock, err error) (*playlock.Lock, error) {
	if err == nil {
		o.Term.Say("The play lock is free after " + playlock.Seconds(o.PlayLock.Now().Sub(w.began)) + "; going on to " + w.purpose + ".")
		return lock, nil
	}
	if gaveUp, ok := errors.AsType[*playlock.TimeoutError](err); ok {
		o.Term.PrintError("gave up waiting for the play lock after " + playlock.Duration(o.PlayLockWait) +
			": it is still held by " + gaveUp.Holder.Describe(o.PlayLock.Now()) + ".")
		return nil, &lockNotTakenError{cause: err}
	}
	if errors.Is(err, errStoppedWaiting) || ctx.Err() != nil {
		o.Term.Say("Stopped waiting for the play lock; nothing was changed.")
		return nil, &lockNotTakenError{cancelled: true, cause: err}
	}
	o.Term.PrintError("the wait for the play lock failed: " + err.Error() + ".")
	return nil, &lockNotTakenError{cause: err}
}

// waitSaying is the wait with no screen: who holds the lock and how long this waits, on stderr,
// and a line each time another run takes the lock in turn.
func (o *Ops) waitSaying(ctx context.Context, w lockWait, holder playlock.Holder) (*playlock.Lock, error) {
	o.Term.Printf("The play lock is held by %s.\n", holder.Describe(w.began))
	o.Term.Printf("ccy-teams waits for it before it can %s: %s. Ctrl-C stops waiting, and changes nothing.\n", w.purpose, o.until(w))
	last := holder.Note
	return o.PlayLock.Take(ctx, w.what, o.PlayLockWait, func(h playlock.Holder, waited time.Duration) {
		if h.Note != last {
			last = h.Note
			o.Term.Printf("The play lock is now held by %s.\n", h.Describe(w.began.Add(waited)))
		}
	})
}

// seenHolder is the holder as the wait last read it, and when.
type seenHolder struct {
	holder playlock.Holder
	at     time.Time
}

// key is what the screen shows of it: a new holder, or another minute held, redraws it.
func (s seenHolder) key() string {
	if s.holder.Since.IsZero() {
		return s.holder.Note
	}
	return s.holder.Note + "\n" + playlock.Duration(s.at.Sub(s.holder.Since))
}

// taken is how the lock's wait ended.
type taken struct {
	lock *playlock.Lock
	err  error
}

// waitOnScreen is the wait on a terminal: a screen saying who holds the lock and until when this
// waits, with a Stop waiting row. The lock is waited for beside it; the screen is closed and
// drawn again when the holder changes or has held the lock another minute, and closed for good
// the moment the lock is taken.
func (o *Ops) waitOnScreen(ctx context.Context, w lockWait, holder playlock.Holder) (*playlock.Lock, error) {
	waitCtx, stopWait := context.WithCancel(ctx)
	defer stopWait()
	seen := make(chan seenHolder)
	done := make(chan taken, 1)
	go func() {
		lock, err := o.PlayLock.Take(waitCtx, w.what, o.PlayLockWait, func(h playlock.Holder, waited time.Duration) {
			select {
			case seen <- seenHolder{holder: h, at: w.began.Add(waited)}:
			case <-waitCtx.Done():
			}
		})
		done <- taken{lock: lock, err: err}
	}()
	shown := seenHolder{holder: holder, at: w.began}
	for {
		screenCtx, closeScreen := context.WithCancel(ctx)
		picked := make(chan error, 1)
		spec := o.lockScreen(w, shown)
		go func() {
			_, err := o.Picker.Pick(screenCtx, spec)
			picked <- err
		}()
		next, end, ok := o.watchScreen(seen, done, picked, shown)
		closeScreen()
		if ok {
			<-picked
			return end.lock, end.err
		}
		if next == nil {
			// The person left the screen, or it failed: the wait stops, and a lock taken in
			// the same instant is let go.
			stopWait()
			got := <-done
			return nil, errors.Join(o.screenLeft(ctx, end.err), got.lock.Release())
		}
		<-picked
		shown = *next
	}
}

// watchScreen waits on the open screen until the wait ends (ok, with how), the screen should be
// drawn again (next), or the screen returned on its own (neither: end.err is why).
func (o *Ops) watchScreen(seen <-chan seenHolder, done <-chan taken, picked <-chan error, shown seenHolder) (*seenHolder, taken, bool) {
	for {
		select {
		case end := <-done:
			return nil, end, true
		case h := <-seen:
			if h.key() != shown.key() {
				return &h, taken{}, false
			}
		case err := <-picked:
			return nil, taken{err: err}, false
		}
	}
}

// screenLeft is why the screen returned before the lock was taken: the person stopping the wait
// (its row, Esc, q or Ctrl-C), or fzf failing.
func (o *Ops) screenLeft(ctx context.Context, err error) error {
	if errors.Is(err, ui.ErrLeave) || ctx.Err() != nil {
		return errStoppedWaiting
	}
	return err
}

// lockScreen is the wait's screen: what it waits for, who holds the lock and for how long, the
// bound, and the one choice, to stop.
func (o *Ops) lockScreen(w lockWait, s seenHolder) ui.PickSpec {
	return ui.PickSpec{
		Title: "Waiting for the play lock",
		Header: ui.Header("Enter choose   Esc or q stop waiting",
			"ccy-teams is waiting for the play lock before it can "+w.purpose+".",
			"Held by "+s.holder.Describe(s.at)+".",
			"It waits "+o.until(w)+", and goes on by itself the moment the lock is free."),
		ExitLabel: "Stop waiting (nothing is changed)",
	}
}

// hasTool is whether name is on PATH.
func (o *Ops) hasTool(name string) bool {
	_, err := o.LookPath(name)
	return err == nil
}
