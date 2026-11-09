package all

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// lockRecorder is the fake QA lock: it records each take and release, and how many gates had
// run at each, so a test sees the lock held around the whole run.
type lockRecorder struct {
	err                     error
	roots                   []string
	noWaits                 []bool
	released                int
	ranAtTake, ranAtRelease int
	ran                     func() int
}

func (l *lockRecorder) take(_ context.Context, root string, noWait bool) (func() error, error) {
	l.roots = append(l.roots, root)
	l.noWaits = append(l.noWaits, noWait)
	l.ranAtTake = l.ran()
	if l.err != nil {
		return nil, l.err
	}
	return func() error {
		l.released++
		l.ranAtRelease = l.ran()
		return nil
	}, nil
}

// freeLock is a lock no other run holds.
func freeLock(context.Context, string, bool) (func() error, error) {
	return func() error { return nil }, nil
}

func TestTheRunHoldsTheQALockFromItsFirstGateToItsVerdict(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	l := w.lock
	if len(l.roots) != 1 || l.roots[0] != w.root || l.noWaits[0] {
		t.Fatalf("takes %q no-wait %v", l.roots, l.noWaits)
	}
	if l.ranAtTake != 0 || l.released != 1 || l.ranAtRelease != len(w.runner.log) {
		t.Fatalf("lock taken after %d gates, released %d times after %d of %d", l.ranAtTake, l.released, l.ranAtRelease, len(w.runner.log))
	}
}

func TestNoWaitIsHandedToTheLock(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if code := w.main(context.Background(), "--no-wait"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.lock.noWaits) != 1 || !w.lock.noWaits[0] {
		t.Fatalf("no-wait %v", w.lock.noWaits)
	}
}

func TestALockNotTakenRunsNoGateAndIsExitTwo(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.lock.err = errors.New("the QA lock is held by the QA run 7 (qa-changed, ...); --no-wait was given")
	if code := w.main(context.Background(), "--no-wait"); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("gates ran without the lock: %q", w.runner.log)
	}
	if !strings.Contains(w.errOut.String(), abortPrefix+"the QA lock is held by the QA run 7") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
}

func TestAnInterruptWhileWaitingForTheLockIsExitInterrupted(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.lock.err = context.Canceled
	if code := w.main(ctx); code != ExitInterrupted {
		t.Fatalf("exit %d", code)
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("gates ran: %q", w.runner.log)
	}
}

func TestHelpTakesNoLock(t *testing.T) {
	w := newWorld(t, smallTable)
	if code := w.main(context.Background(), "--help"); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "--no-wait") {
		t.Fatalf("the usage does not name --no-wait:\n%s", w.out.String())
	}
	if len(w.lock.roots) != 0 {
		t.Fatal("--help took the lock")
	}
}

func TestARunWithNoLockWiredIsRefused(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	env := w.env0()
	env.Lock = nil
	if code := Main(context.Background(), []string{"--root", w.root}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if len(w.runner.log) != 0 {
		t.Fatal("gates ran with no lock")
	}
}
