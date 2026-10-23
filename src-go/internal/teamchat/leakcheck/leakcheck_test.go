package leakcheck

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) { os.Exit(Tests(m).Run()) }

// quick settles in a few milliseconds, so a leak is reported without the real wait.
var quick = settle{tries: 3, pause: time.Millisecond}

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

// parked is a goroutine blocked until release closes.
func parked(release <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	<-release
}

func TestCheckReportsAGoroutineLeftRunning(t *testing.T) {
	tb := &fakeTB{TB: t}
	check(tb, quick)
	release, done := make(chan struct{}), make(chan struct{})
	go parked(release, done)
	tb.end()
	close(release)
	<-done
	if len(tb.errs) != 1 || !strings.Contains(tb.errs[0], "leakcheck.parked") {
		t.Fatalf("errors %q", tb.errs)
	}
}

func TestCheckPassesAGoroutineThatHasEnded(t *testing.T) {
	tb := &fakeTB{TB: t}
	check(tb, quick)
	release, done := make(chan struct{}), make(chan struct{})
	go parked(release, done)
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
	check(tb, settle{tries: 1000, pause: time.Millisecond})
	release, done := make(chan struct{}), make(chan struct{})
	go parked(release, done)
	tb.Cleanup(func() { close(release) })
	tb.end()
	<-done
	if len(tb.errs) != 0 {
		t.Fatalf("errors %q", tb.errs)
	}
}

func TestCheckIgnoresAGoroutineStartedBeforeIt(t *testing.T) {
	release, done := make(chan struct{}), make(chan struct{})
	go parked(release, done)
	tb := &fakeTB{TB: t}
	check(tb, quick)
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
		go parked(r.release, r.stopped)
	}
	return r.code
}

func TestTestsFailsAPassingRunThatLeavesAGoroutine(t *testing.T) {
	r := &runner{leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	var out strings.Builder
	code := tests{m: r, out: &out, settle: quick}.Run()
	close(r.release)
	<-r.stopped
	if code != 1 || !strings.Contains(out.String(), "leakcheck.parked") || !strings.Contains(out.String(), "still running") {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestTestsKeepsTheCodeOfAFailedRun(t *testing.T) {
	r := &runner{code: 3, leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	var out strings.Builder
	code := tests{m: r, out: &out, settle: quick}.Run()
	close(r.release)
	<-r.stopped
	if code != 3 || out.Len() != 0 {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestTestsPassesACleanRun(t *testing.T) {
	var out strings.Builder
	if code := (tests{m: &runner{}, out: &out, settle: quick}).Run(); code != 0 || out.Len() != 0 {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

// A report that cannot be written still fails the run.
func TestTestsFailsWhenTheReportCannotBeWritten(t *testing.T) {
	r := &runner{leave: true, release: make(chan struct{}), stopped: make(chan struct{})}
	code := tests{m: r, out: failingWriter{}, settle: quick}.Run()
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
