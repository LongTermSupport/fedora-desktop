package changed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qalock"
)

// freeLock is a lock no other run holds.
func freeLock(context.Context, string, bool) (func() error, error) {
	return func() error { return nil }, nil
}

// lockRecorder is the fake QA lock: each take and release, and how many commands had run at
// each, so a test sees the lock held around every check.
type lockRecorder struct {
	err                     error
	roots                   []string
	noWaits                 []bool
	released                int
	ranAtTake, ranAtRelease int
}

func (l *lockRecorder) take(ran func() int) qalock.Taker {
	return func(_ context.Context, root string, noWait bool) (func() error, error) {
		l.roots = append(l.roots, root)
		l.noWaits = append(l.noWaits, noWait)
		l.ranAtTake = ran()
		if l.err != nil {
			return nil, l.err
		}
		return func() error {
			l.released++
			l.ranAtRelease = ran()
			return nil
		}, nil
	}
}

// checkCalls is how many commands were not git: git collects the change, the rest are checks.
func checkCalls(w *runWorld) int {
	n := 0
	for _, c := range w.runner.calls {
		if c.Argv[0] != "gi"+"t" {
			n++
		}
	}
	return n
}

func TestTheChecksRunUnderTheQALock(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	l := &w.lock
	if len(l.roots) != 1 || l.roots[0] != w.root || l.noWaits[0] || l.released != 1 {
		t.Fatalf("takes %q no-wait %v released %d", l.roots, l.noWaits, l.released)
	}
	if l.ranAtRelease != len(w.runner.calls) {
		t.Fatalf("released after %d of %d commands", l.ranAtRelease, len(w.runner.calls))
	}
	// Only git (the collection) ran before the lock was taken.
	before := w.runner.calls[:l.ranAtTake]
	for _, c := range before {
		if c.Argv[0] != "gi"+"t" {
			t.Fatalf("a check ran before the lock: %q", c.Argv)
		}
	}
	if checkCalls(w) == 0 {
		t.Fatal("no check ran")
	}
}

func TestNoWaitIsHandedToTheLock(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main("--no-wait"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.lock.noWaits) != 1 || !w.lock.noWaits[0] {
		t.Fatalf("no-wait %v", w.lock.noWaits)
	}
}

func TestALockNotTakenRunsNoCheckAndIsExitTwo(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.lock.err = errors.New("the QA lock is held by the QA run 7 (qa-all, ...); --no-wait was given")
	if code := w.main("--no-wait"); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if n := checkCalls(w); n != 0 {
		t.Fatalf("%d checks ran without the lock", n)
	}
	if !strings.Contains(w.errOut.String(), "ERROR: the QA lock is held by the QA run 7") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
}

func TestAnInterruptWhileWaitingForTheLockIsExitInterrupted(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.lock.err = context.Canceled
	if code := w.main(); code != ExitInterrupted {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if n := checkCalls(w); n != 0 {
		t.Fatalf("%d checks ran", n)
	}
}

func TestAPlanOrANothingChangedRunTakesNoLock(t *testing.T) {
	w := newRunWorld(t)
	if code := w.main(); code != ExitOK {
		t.Fatalf("nothing changed: exit %d", code)
	}
	w.changeKept()
	if code := w.main("--plan"); code != ExitOK {
		t.Fatalf("--plan: exit %d", code)
	}
	if code := w.main("--help"); code != ExitOK || !strings.Contains(w.out.String(), "--no-wait") {
		t.Fatalf("--help: exit %d, usage without --no-wait:\n%s", code, w.out.String())
	}
	if len(w.lock.roots) != 0 {
		t.Fatalf("took the lock %d times", len(w.lock.roots))
	}
}

func TestARunWithNoLockWiredIsRefused(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	env := Env{Getenv: func(string) string { return "" }, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }}
	if code := Main(context.Background(), []string{"--scripts", w.scripts, "--root", w.root}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if n := checkCalls(w); n != 0 {
		t.Fatalf("%d checks ran with no lock", n)
	}
}
