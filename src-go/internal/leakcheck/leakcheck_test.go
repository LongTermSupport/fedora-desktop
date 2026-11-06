package leakcheck

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

func TestMain(m *testing.M) { os.Exit(Tests(m).Run()) }

// once is one reading and no pause, for a goroutine the test keeps parked past the reading: it
// is reported without a wait, and no wait could change the verdict. A test whose goroutine
// must end first settles as the standard does, on its real clock.
var once = settle{tries: 1}

// The standard settle is five seconds on the system clock.
func TestTheStandardSettleIsFiveSecondsOfTheSystemClock(t *testing.T) {
	if got := time.Duration(standard.tries) * standard.pause; got != 5*time.Second || standard.clock != clock.System() {
		t.Fatalf("standard settles %v on %v", got, standard.clock)
	}
}

// The pause between readings is slept on the settle's clock.
func TestTheSettlePausesOnItsClock(t *testing.T) {
	clk := clock.NewFake(0, 0)
	s := settle{tries: 3, pause: time.Second, clock: clk}
	release, done := make(chan struct{}), make(chan struct{})
	before := ids()
	park(release, done)
	left := make(chan []goroutine, 1)
	go func() { left <- s.leftSince(before) }()
	for range s.tries - 1 {
		if err := clk.AwaitDue(t.Context(), s.pause); err != nil {
			t.Fatal(err)
		}
		clk.Advance(s.pause)
	}
	got := <-left
	close(release)
	<-done
	if len(got) != 1 || !strings.Contains(got[0].stack, "leakcheck.parked") {
		t.Fatalf("left %+v", got)
	}
}

// fakeTB records what Check reports and holds its cleanups for the test to run.
type fakeTB struct {
	testing.TB

	errs     []string
	cleanups []func()
}

func (*fakeTB) Helper() {}

func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

// Fatal records the call and ends the goroutine it is called on, as testing.T's does.
func (f *fakeTB) Fatal(args ...any) {
	f.errs = append(f.errs, fmt.Sprint(args...))
	runtime.Goexit()
}

func (f *fakeTB) end() {
	for _, fn := range slices.Backward(f.cleanups) {
		fn()
	}
}

// park starts parked and returns once it is running in it, so a reading names it by its function
// (a goroutine not yet scheduled shows only its go statement's wrapper).
func park(release <-chan struct{}, done chan<- struct{}) {
	running := make(chan struct{})
	go parked(running, release, done)
	<-running
}

// parked is a goroutine blocked until release closes.
func parked(running chan<- struct{}, release <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	close(running)
	<-release
}

func TestCheckReportsAGoroutineLeftRunning(t *testing.T) {
	tb := &fakeTB{TB: t}
	check(tb, once)
	release, done := make(chan struct{}), make(chan struct{})
	park(release, done)
	tb.end()
	close(release)
	<-done
	if len(tb.errs) != 1 || !strings.Contains(tb.errs[0], "leakcheck.parked") {
		t.Fatalf("errors %q", tb.errs)
	}
}

func TestCheckPassesAGoroutineThatHasEnded(t *testing.T) {
	tb := &fakeTB{TB: t}
	check(tb, standard)
	release, done := make(chan struct{}), make(chan struct{})
	park(release, done)
	close(release)
	<-done
	tb.end()
	if len(tb.errs) != 0 {
		t.Fatalf("errors %q", tb.errs)
	}
}

// A goroutine still ending when the test ends is waited for, not reported.
func TestCheckWaitsForAGoroutineThatIsEnding(t *testing.T) {
	tb := &fakeTB{TB: t}
	check(tb, standard)
	release, done := make(chan struct{}), make(chan struct{})
	park(release, done)
	tb.Cleanup(func() { close(release) })
	tb.end()
	<-done
	if len(tb.errs) != 0 {
		t.Fatalf("errors %q", tb.errs)
	}
}

func TestCheckIgnoresAGoroutineStartedBeforeIt(t *testing.T) {
	release, done := make(chan struct{}), make(chan struct{})
	park(release, done)
	tb := &fakeTB{TB: t}
	check(tb, once)
	tb.end()
	close(release)
	<-done
	if len(tb.errs) != 0 {
		t.Fatalf("errors %q", tb.errs)
	}
}

// runner is a test run that returns code, leaving a parked goroutine when leave is set.
type runner struct {
	code             int
	leave            bool
	release, stopped chan struct{}
}

func (r *runner) Run() int {
	if r.leave {
		park(r.release, r.stopped)
	}
	return r.code
}

func TestTestsFailsAPassingRunThatLeavesAGoroutine(t *testing.T) {
	r := &runner{leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	var out strings.Builder
	code := tests{m: r, out: &out, settle: once}.Run()
	close(r.release)
	<-r.stopped
	if code != 1 || !strings.Contains(out.String(), "leakcheck.parked") || !strings.Contains(out.String(), "still running") {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestTestsKeepsTheCodeOfAFailedRun(t *testing.T) {
	r := &runner{code: 3, leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	var out strings.Builder
	code := tests{m: r, out: &out, settle: once}.Run()
	close(r.release)
	<-r.stopped
	if code != 3 || out.Len() != 0 {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestTestsPassesACleanRun(t *testing.T) {
	var out strings.Builder
	if code := (tests{m: &runner{}, out: &out, settle: standard}).Run(); code != 0 || out.Len() != 0 {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

// A report that cannot be written still fails the run.
func TestTestsFailsWhenTheReportCannotBeWritten(t *testing.T) {
	r := &runner{leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	code := tests{m: r, out: failingWriter{}, settle: once}.Run()
	close(r.release)
	<-r.stopped
	if code != 1 {
		t.Fatalf("code %d", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestTheStacksAreSplitByGoroutine(t *testing.T) {
	dump := "goroutine 1 [running]:\nmain.main()\n\ngoroutine 7 [chan receive]:\nx.y()\n\tx.go:3\n"
	got := parse(dump)
	if len(got) != 2 || got[0].id != 1 || got[1].id != 7 || !strings.Contains(got[1].stack, "x.go:3") {
		t.Fatalf("parsed %+v", got)
	}
	if bad := parse("goroutine x [running]:\n"); len(bad) != 0 {
		t.Fatalf("parsed %+v", bad)
	}
}

func TestCheckRegistersOnARealTest(t *testing.T) {
	Check(t)
}

// Only a wait that another goroutine must end is blocked; every state in which a goroutine can
// make progress by itself (running, runnable, a system call, the network poller, asleep) is not,
// and a header that names no state is not read as blocked.
func TestAGoroutineIsBlockedOnlyInTheWaitsAnotherEnds(t *testing.T) {
	for _, c := range []struct {
		state   string
		blocked bool
	}{
		{"chan receive", true},
		{"chan receive, 3 minutes", true},
		{"chan send", true},
		{"select", true},
		{"select, locked to thread", true},
		{"select (no cases)", true},
		{"sync.Mutex.Lock", true},
		{"sync.WaitGroup.Wait, 1 minutes", true},
		{"sync.Cond.Wait", true},
		{"sync.RWMutex.RLock", true},
		{"semacquire", false},
		{"chan receive (durable)", true},
		{"synctest.Run (durable)", true},
		{"running", false},
		{"runnable", false},
		{"syscall", false},
		{"IO wait", false},
		{"sleep", false},
		{"sleep (synctest)", false},
		{"GC assist wait", false},
		{"finalizer wait", false},
	} {
		g := goroutine{id: 7, stack: "goroutine 7 [" + c.state + "]:\nx.y()\n\tx.go:3"}
		if got := g.blocked(); got != c.blocked {
			t.Errorf("[%s] blocked = %v, want %v", c.state, got, c.blocked)
		}
	}
	if (goroutine{id: 7, stack: "goroutine 7"}).blocked() {
		t.Error("a header with no state was read as blocked")
	}
}

// idleSoon is Idle once the goroutines of earlier tests have finished ending. A test's goroutine is
// still running for a moment after it reports, outside a synctest bubble, so a single reading made
// right after the previous test can find it and read false (a few iterations in a hundred when the
// test before has no goroutine of its own to give it time). The yield lets it end; it is a range
// loop, which ends by itself, and the count is far past the few turns an ending goroutine needs.
func idleSoon() bool {
	for range 10000 {
		if Idle() {
			return true
		}
		runtime.Gosched()
	}
	return false
}

// Idle is true once every other goroutine is parked, and false while one is asleep (a timer
// ends its wait) or running.
func TestIdleWhenEveryOtherGoroutineIsParked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release, done := make(chan struct{}), make(chan struct{})
		park(release, done)
		synctest.Wait()
		idle := idleSoon()
		close(release)
		<-done
		if !idle {
			t.Fatal("Idle was false with the one other goroutine parked")
		}
	})
}

func TestNotIdleWhileAGoroutineSleeps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(time.Hour)
		}()
		synctest.Wait()
		idle := Idle()
		time.Sleep(time.Hour)
		<-done
		if idle {
			t.Fatal("Idle was true with a goroutine asleep")
		}
	})
}

// Poll returns as soon as its condition holds, however long that takes the goroutine that makes
// it hold.
func TestPollReturnsOnceTheConditionHolds(t *testing.T) {
	var n atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Store(1)
	}()
	Poll(t, func() bool { return n.Load() == 1 })
	<-done
}

// A condition that takes what it finds (a receive) holds once. When the read after the idle reading
// is the one that finds it, that read ends the wait: reading again would find nothing and fail a
// test whose condition held.
func TestPollEndsOnTheReadAfterTheIdleReadingWithoutReadingAgain(t *testing.T) {
	if !idleSoon() {
		t.Fatal("the world never went idle")
	}
	reads := 0
	Poll(t, func() bool {
		reads++
		return reads == 2
	})
	if reads != 2 {
		t.Fatalf("reads = %d, want 2", reads)
	}
}

// With every other goroutine blocked and the condition false, Poll fails the test, once.
func TestPollFailsWhenNothingCanMakeTheConditionHold(t *testing.T) {
	tb := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Poll(tb, func() bool { return false })
	}()
	<-done
	if len(tb.errs) != 1 || !strings.Contains(tb.errs[0], "every other goroutine is blocked") {
		t.Fatalf("errors %q", tb.errs)
	}
}

// Drive acts only on a world that is idle with its condition false: with every other goroutine
// blocked it calls the step, and returns once the step has made the condition hold.
func TestDriveStepsAnIdleWorldUntilTheConditionHolds(t *testing.T) {
	var n atomic.Int64
	steps := 0
	Drive(t, func() bool { return n.Load() >= 3 }, func() {
		steps++
		n.Add(1)
	})
	if steps != 3 {
		t.Fatalf("steps = %d, want 3", steps)
	}
}

// A condition that holds at the second read, taken after the idle reading, is not stepped: the
// verdict on an idle world is made on a read nothing has run between.
func TestDriveDoesNotStepWhenTheSecondReadHolds(t *testing.T) {
	reads, steps := 0, 0
	Drive(t, func() bool {
		reads++
		return reads >= 2
	}, func() { steps++ })
	if reads < 2 || steps != 0 {
		t.Fatalf("reads = %d, steps = %d, want at least 2 and 0", reads, steps)
	}
}

// Drive ends on the read after the idle reading too, without reading again (a receive holds once).
func TestDriveEndsOnTheReadAfterTheIdleReadingWithoutReadingAgain(t *testing.T) {
	if !idleSoon() {
		t.Fatal("the world never went idle")
	}
	reads, steps := 0, 0
	Drive(t, func() bool {
		reads++
		return reads == 2
	}, func() { steps++ })
	if reads != 2 || steps != 0 {
		t.Fatalf("reads = %d, steps = %d, want 2 and 0", reads, steps)
	}
}

// While a goroutine runs the world is not idle, so the step is not called: the goroutine's own
// progress is what the wait is for. The goroutine computes without blocking or yielding, for a
// fixed count of iterations that ends by itself, so Drive's readings find it running or runnable
// until it stores.
func TestDriveDoesNotStepWhileAGoroutineRuns(t *testing.T) {
	var n, work atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 300_000 {
			work.Add(1)
		}
		n.Store(1)
	}()
	Drive(t, func() bool { return n.Load() == 1 }, func() { t.Error("stepped while a goroutine was running") })
	<-done
}

// A step that never makes the condition hold fails the test after the most steps Drive takes, on
// a world that was idle every time: a count of acts, not of time, so a loaded host does not
// shorten it.
func TestDriveFailsWhenTheStepsNeverMakeTheConditionHold(t *testing.T) {
	tb := &fakeTB{TB: t}
	steps := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		Drive(tb, func() bool { return false }, func() { steps++ })
	}()
	<-done
	if steps != driveMaxSteps || len(tb.errs) != 1 || !strings.Contains(tb.errs[0], "steps") {
		t.Fatalf("steps = %d, errors %q", steps, tb.errs)
	}
}

func TestNotIdleWhileAGoroutineRuns(t *testing.T) {
	var stop atomic.Bool
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		for !stop.Load() {
			continue
		}
	}()
	<-started
	idle := Idle()
	stop.Store(true)
	<-done
	if idle {
		t.Fatal("Idle was true with a goroutine running")
	}
}
