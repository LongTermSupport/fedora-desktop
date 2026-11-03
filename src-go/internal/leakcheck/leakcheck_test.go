package leakcheck

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
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
