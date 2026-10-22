package clock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The clock meets every seam it stands in for, structurally: clock imports none of them.
var (
	_ lock.Clock   = clock.Clock(nil)
	_ matrix.Clock = clock.Clock(nil)
	// matrix.Timers (P1, gap 12) has this method set; W replaces this with matrix.Timers.
	_ interface {
		AfterFunc(d time.Duration, f func()) (stop func() bool)
	} = clock.Clock(nil)
	_ clock.Clock = clock.System()
	_ clock.Clock = (*clock.Fake)(nil)
)

// hold is fakehs.Options.Hold's type (P1, gap 12).
type hold func(d time.Duration) (<-chan time.Time, func() bool)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// await is the fake's waiters reaching n, bounded by the test's own context.
func await(t *testing.T, f *clock.Fake, n int) {
	t.Helper()
	check(t, f.BlockUntil(t.Context(), n))
}

func TestFakeReadsWhatItWasGivenAndAdvanceMovesEveryClock(t *testing.T) {
	f := clock.NewFake(10*time.Second, 1_760_000_000_000)
	if f.Boot() != 10*time.Second || f.Mono() != 10*time.Second || f.WallMS() != 1_760_000_000_000 {
		t.Fatalf("start: boot %v mono %v wall %d", f.Boot(), f.Mono(), f.WallMS())
	}
	f.Advance(1500 * time.Millisecond)
	if f.Boot() != 11500*time.Millisecond || f.Mono() != 11500*time.Millisecond || f.WallMS() != 1_760_000_001_500 {
		t.Fatalf("advanced: boot %v mono %v wall %d", f.Boot(), f.Mono(), f.WallMS())
	}
}

func TestSuspendMovesBootAndWallButNotMono(t *testing.T) {
	f := clock.NewFake(time.Minute, 1000)
	f.Suspend(time.Hour)
	if f.Boot() != time.Hour+time.Minute || f.Mono() != time.Minute || f.WallMS() != 1000+time.Hour.Milliseconds() {
		t.Fatalf("boot %v mono %v wall %d", f.Boot(), f.Mono(), f.WallMS())
	}
}

func TestStepWallMovesTheWallAlone(t *testing.T) {
	f := clock.NewFake(time.Minute, 5000)
	f.StepWall(-4000)
	if f.WallMS() != 1000 || f.Boot() != time.Minute || f.Mono() != time.Minute {
		t.Fatalf("boot %v mono %v wall %d", f.Boot(), f.Mono(), f.WallMS())
	}
}

func TestSleepReturnsWhenTheClockReachesItsEnd(t *testing.T) {
	f := clock.NewFake(0, 0)
	done := make(chan error, 1)
	go func() { done <- f.Sleep(t.Context(), time.Second) }()
	await(t, f, 1)
	f.Advance(999 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("woke early: %v", err)
	default:
	}
	f.Advance(time.Millisecond)
	check(t, <-done)
	if f.Waiters() != 0 {
		t.Fatalf("%d waiters after the sleep ended", f.Waiters())
	}
}

func TestSleepEndsWithItsContext(t *testing.T) {
	f := clock.NewFake(0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.Sleep(ctx, time.Hour) }()
	await(t, f, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the context's error", err)
	}
	await(t, f, 0)
}

func TestSleepOnAnEndedContextOrForNothing(t *testing.T) {
	f := clock.NewFake(0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.Sleep(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("ended context: %v", err)
	}
	check(t, f.Sleep(t.Context(), 0))
	if f.Waiters() != 0 {
		t.Fatalf("%d waiters", f.Waiters())
	}
}

func TestAfterFuncFiresOnceInDueOrderAndStopDisarms(t *testing.T) {
	f := clock.NewFake(time.Second, 0)
	var fired []string
	var at []time.Duration
	f.AfterFunc(2*time.Second, func() { fired = append(fired, "b"); at = append(at, f.Boot()) })
	f.AfterFunc(time.Second, func() { fired = append(fired, "a"); at = append(at, f.Boot()) })
	stop := f.AfterFunc(1500*time.Millisecond, func() { fired = append(fired, "stopped") })
	if f.Waiters() != 3 {
		t.Fatalf("%d waiters, want 3", f.Waiters())
	}
	if !stop() || stop() {
		t.Fatal("stop: true once, then false")
	}
	f.Advance(5 * time.Second)
	if len(fired) != 2 || fired[0] != "a" || fired[1] != "b" {
		t.Fatalf("fired %v", fired)
	}
	// Each fires with the clock at its own due time, so a callback reads that time.
	if at[0] != 2*time.Second || at[1] != 3*time.Second || f.Boot() != 6*time.Second {
		t.Fatalf("at %v, now %v", at, f.Boot())
	}
	f.Advance(time.Hour)
	if len(fired) != 2 {
		t.Fatalf("fired again: %v", fired)
	}
}

func TestATimerArmedByACallbackFiresInTheSameAdvanceWhenDue(t *testing.T) {
	f := clock.NewFake(0, 0)
	n := 0
	var again func()
	again = func() {
		n++
		f.AfterFunc(time.Second, again)
	}
	f.AfterFunc(time.Second, again)
	f.Advance(3 * time.Second)
	if n != 3 || f.Waiters() != 1 {
		t.Fatalf("fired %d times, %d waiters", n, f.Waiters())
	}
}

// Timers run on the monotonic clock, as the runtime's do: a suspend fires none of them.
func TestSuspendFiresNoTimer(t *testing.T) {
	f := clock.NewFake(0, 0)
	fired := false
	f.AfterFunc(time.Second, func() { fired = true })
	f.Suspend(time.Hour)
	if fired {
		t.Fatal("a suspend fired a timer")
	}
	f.Advance(time.Second)
	if !fired {
		t.Fatal("not fired once the monotonic clock reached it")
	}
}

func TestHoldDeliversTheWallTimeAndStops(t *testing.T) {
	f := clock.NewFake(0, 1_760_000_000_000)
	var h hold = f.Hold
	c, stop := h(time.Second)
	never, stopNever := h(time.Second)
	if !stopNever() {
		t.Fatal("stop of an armed hold is true")
	}
	f.Advance(time.Second)
	got := <-c
	if got.UnixMilli() != 1_760_000_001_000 {
		t.Fatalf("hold delivered %v", got)
	}
	if stop() {
		t.Fatal("stop of a fired hold is false")
	}
	select {
	case <-never:
		t.Fatal("a stopped hold delivered")
	default:
	}
}

func TestBlockUntilEndsWithItsContext(t *testing.T) {
	f := clock.NewFake(0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.BlockUntil(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	check(t, f.BlockUntil(t.Context(), 0))
}

// lock's three tries, 50 ms apart, run on the fake: the acquire sleeps twice, then is busy.
func TestTheFakeDrivesLocksThreeTries(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	check(t, os.Mkdir(home, 0o700))
	root, err := homefs.Open(home, homefs.Private)
	check(t, err)
	t.Cleanup(func() { check(t, root.Close()) })
	layout, err := paths.HomeLayout(home, home+"/runtime")
	check(t, err)
	runID, err := paths.ParseRunID("0123456789abcdef")
	check(t, err)
	run := layout.Run(runID)
	check(t, root.MkdirAll(run.Dir()))
	f := clock.NewFake(0, 0)
	held, err := lock.TakeLink(t.Context(), f, root, run)
	check(t, err)
	t.Cleanup(func() { check(t, held.Release()) })

	done := make(chan error, 1)
	go func() {
		second, err := lock.TakeLink(t.Context(), f, root, run)
		if err == nil {
			err = errors.Join(errors.New("taken twice"), second.Release())
		}
		done <- err
	}()
	for range 2 {
		await(t, f, 1)
		f.Advance(50 * time.Millisecond)
	}
	if err := <-done; !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("got %v, want busy after three tries", err)
	}
	if f.Boot() != 100*time.Millisecond {
		t.Fatalf("waited %v, want two gaps", f.Boot())
	}
}

// The system clock's readings: boot never behind mono, since only a suspend parts them.
func TestSystemClockReadings(t *testing.T) {
	c := clock.System()
	mono := c.Mono()
	boot := c.Boot()
	if mono <= 0 || boot < mono {
		t.Fatalf("boot %v mono %v", boot, mono)
	}
	if c.WallMS() <= 0 {
		t.Fatalf("wall %d", c.WallMS())
	}
}

func TestSystemClockWaits(t *testing.T) {
	c := clock.System()
	check(t, c.Sleep(t.Context(), time.Millisecond))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	fired := make(chan struct{})
	c.AfterFunc(time.Millisecond, func() { close(fired) })
	<-fired
	if stop := c.AfterFunc(time.Hour, func() {}); !stop() {
		t.Fatal("stop of an armed timer is true")
	}
	soon, stopSoon := c.Hold(time.Millisecond)
	<-soon
	if stopSoon() {
		t.Fatal("stop of a fired hold is false")
	}
	late, stopLate := c.Hold(time.Hour)
	if !stopLate() || len(late) != 0 {
		t.Fatal("stop of an armed hold is true and delivers nothing")
	}
}
