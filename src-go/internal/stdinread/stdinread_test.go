package stdinread

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// syncBuffer is a stderr the reader and the test both touch.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// pipe is an open pipe whose writer the test holds: the shape of a command run with no input in
// an agent's shell. Both ends are closed when the test ends, which lets a reader given up on end.
func pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, w
}

// silent is the reading end of an open pipe nothing will write to before the test ends.
func silent(t *testing.T) *os.File {
	t.Helper()
	r, w := pipe(t)
	if w == nil {
		t.Fatal("no writer")
	}
	return r
}

// answer is one Read's answer.
type answer struct {
	data []byte
	err  error
}

// start runs Read in the background; the answer arrives on the channel.
func start(r io.Reader, o Options) <-chan answer {
	got := make(chan answer, 1)
	go func() {
		data, err := Read(r, o)
		got <- answer{data, err}
	}()
	return got
}

// await is the answer, failing the test if none comes while the test can still report it.
func await(t *testing.T, got <-chan answer) answer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	select {
	case r := <-got:
		return r
	case <-ctx.Done():
		t.Fatal("Read never returned")
		return answer{}
	}
}

// due waits until the fake has a timer armed at d, then advances to it.
func due(t *testing.T, f *clock.Fake, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := f.AwaitDue(ctx, d); err != nil {
		t.Fatal(err)
	}
	f.Advance(d)
}

func notTerminal(any) bool { return false }

// Input that is there is read whole and nothing is said.
func TestInputThatIsThereIsReadAndNothingIsSaid(t *testing.T) {
	var errs syncBuffer
	data, err := Read(strings.NewReader("hello\n"), Options{Prog: "tool", Err: &errs, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal})
	if err != nil || string(data) != "hello\n" || errs.String() != "" {
		t.Fatalf("%q %v %q", data, err, errs.String())
	}
}

// At most Limit bytes are read; the caller judges a read that reached it as over its cap.
func TestAtMostTheLimitIsRead(t *testing.T) {
	data, err := Read(strings.NewReader("abcdef"), Options{Limit: 4, Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal})
	if err != nil || string(data) != "abcd" {
		t.Fatalf("%q %v", data, err)
	}
}

// An open pipe nothing writes to: after NoticeAfter the reader says on stderr that it is waiting
// and how to end the input, then gives up after GiveUpAfter, naming the bound.
func TestAnOpenPipeIsNoticedThenGivenUp(t *testing.T) {
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	got := start(silent(t), Options{Prog: "teamchat say", Err: &errs, Clock: f, GiveUpAfter: time.Minute, IsTerminal: notTerminal})
	due(t, f, NoticeAfter)
	due(t, f, time.Minute-NoticeAfter)
	res := await(t, got)
	var none *NoInputError
	if !errors.As(res.err, &none) || none.After != time.Minute || res.err.Error() != "no input arrived on stdin within 60 s" {
		t.Fatalf("%v", res.err)
	}
	want := "teamchat say: waiting for input on stdin, which has not ended; pipe it in, or end it (ctrl-D, or < /dev/null for none)\n"
	if errs.String() != want {
		t.Fatalf("stderr %q, want %q", errs.String(), want)
	}
}

// With no give-up the reader says it is waiting, once, and waits until the input ends.
func TestWithNoBoundTheNoticeIsOnceAndTheReadWaitsForTheEnd(t *testing.T) {
	r, w := pipe(t)
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	got := start(r, Options{Prog: "tool", Err: &errs, Clock: f, IsTerminal: notTerminal})
	due(t, f, NoticeAfter)
	if _, err := w.WriteString("late\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	res := await(t, got)
	if res.err != nil || string(res.data) != "late\n" || strings.Count(errs.String(), "waiting for input") != 1 {
		t.Fatalf("%q %v %q", res.data, res.err, errs.String())
	}
}

// Input that has started is never given up on: the bound is on silence, not on a slow writer.
func TestStartedInputIsNotGivenUp(t *testing.T) {
	r, w := pipe(t)
	f := clock.NewFake(0, 0)
	started := make(chan struct{})
	o := Options{Err: io.Discard, Clock: f, GiveUpAfter: time.Minute, IsTerminal: notTerminal, onStart: func() { close(started) }}
	got := start(r, o)
	if _, err := w.WriteString("part"); err != nil {
		t.Fatal(err)
	}
	<-started
	f.Advance(2 * time.Minute)
	if _, err := w.WriteString(" rest"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	res := await(t, got)
	if res.err != nil || string(res.data) != "part rest" {
		t.Fatalf("%q %v", res.data, res.err)
	}
}

// An empty input that ends is an empty read, not a wait.
func TestAnInputThatEndsEmptyIsAnEmptyRead(t *testing.T) {
	r, w := pipe(t)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := Read(r, Options{Err: io.Discard, Clock: clock.NewFake(0, 0), GiveUpAfter: time.Minute, IsTerminal: notTerminal})
	if err != nil || len(data) != 0 {
		t.Fatalf("%q %v", data, err)
	}
}

// A terminal is refused before any read when asked, and read when not.
func TestATerminalIsRefusedWhenAsked(t *testing.T) {
	terminal := func(any) bool { return true }
	_, err := Read(strings.NewReader("typed"), Options{Err: io.Discard, Clock: clock.NewFake(0, 0), RefuseTerminal: true, IsTerminal: terminal})
	if !errors.Is(err, ErrTerminal) || err.Error() != "stdin is a terminal; give the input with a pipe or a file (< FILE)" {
		t.Fatalf("%v", err)
	}
	data, err := Read(strings.NewReader("typed"), Options{Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: terminal})
	if err != nil || string(data) != "typed" {
		t.Fatalf("%q %v", data, err)
	}
}

// A read that fails is the reader's error, wrapped.
func TestAFailedReadIsAnError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Read(io.MultiReader(strings.NewReader("x"), failing{boom}), Options{Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "read stdin") {
		t.Fatalf("%v", err)
	}
}

type failing struct{ err error }

func (f failing) Read([]byte) (int, error) { return 0, f.err }

// The defaults: the real clock, the real terminal test and stderr, so a caller names only its
// program. With input present the real clock is never waited on.
func TestTheDefaultsReadInputThatIsThere(t *testing.T) {
	r, w := pipe(t)
	if _, err := w.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := Read(r, Options{Prog: "tool"})
	if err != nil || string(data) != "x" {
		t.Fatalf("%q %v", data, err)
	}
}

// Await is the shared wait: what arrives is returned as it came, with nothing said.
func TestAwaitReturnsWhatArrives(t *testing.T) {
	arrived := make(chan string, 1)
	arrived <- "yes"
	var errs syncBuffer
	got, err := Await(Options{Err: &errs, Clock: clock.NewFake(0, 0), GiveUpAfter: time.Minute}, arrived, nil)
	if err != nil || got != "yes" || errs.String() != "" {
		t.Fatalf("%q %v %q", got, err, errs.String())
	}
}

// Await says it is waiting after NoticeAfter and refuses after GiveUpAfter.
func TestAwaitNoticesThenGivesUp(t *testing.T) {
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	got := make(chan error, 1)
	go func() {
		_, err := Await(Options{Prog: "ask", Err: &errs, Clock: f, GiveUpAfter: time.Minute}, make(chan string), nil)
		got <- err
	}()
	due(t, f, NoticeAfter)
	due(t, f, time.Minute-NoticeAfter)
	var none *NoInputError
	if err := <-got; !errors.As(err, &none) || !strings.HasPrefix(errs.String(), "ask: waiting for input on stdin") {
		t.Fatalf("%v %q", err, errs.String())
	}
}

// Await ends when stop is closed, with ErrStopped and nothing said.
// passedClock is a clock on which every hold has already passed when Await first looks, as
// after a suspended machine, or a test that moves a fake clock past both holds before the
// waiter reaches its select.
type passedClock struct{}

func (passedClock) Hold(time.Duration) (<-chan time.Time, func() bool) {
	c := make(chan time.Time, 1)
	c <- time.Time{}
	return c, func() bool { return false }
}

// The notice and the bound both passed together still give the notice before the error: the
// error never comes without it. Repeated, because select picks at random among cases ready
// together.
func TestABoundPassedWithTheNoticeStillGivesTheNoticeFirst(t *testing.T) {
	for range 20 {
		var errs syncBuffer
		_, err := Await(Options{Prog: "ask", Err: &errs, Clock: passedClock{}, GiveUpAfter: time.Minute}, make(chan string), nil)
		var none *NoInputError
		if !errors.As(err, &none) || !strings.HasPrefix(errs.String(), "ask: waiting for input on stdin") {
			t.Fatalf("%v %q", err, errs.String())
		}
	}
}

func TestAwaitEndsOnStop(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	var errs syncBuffer
	_, err := Await(Options{Err: &errs, Clock: clock.NewFake(0, 0)}, make(chan int), stop)
	if !errors.Is(err, ErrStopped) || errs.String() != "" {
		t.Fatalf("%v %q", err, errs.String())
	}
}

// Without a program name the notice names stdin alone.
func TestTheNoticeWithoutAProgramName(t *testing.T) {
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	got := start(silent(t), Options{Err: &errs, Clock: f, GiveUpAfter: 2 * NoticeAfter, IsTerminal: notTerminal})
	due(t, f, NoticeAfter)
	due(t, f, NoticeAfter)
	if res := await(t, got); res.err == nil || !strings.HasPrefix(errs.String(), "waiting for input on stdin") {
		t.Fatalf("%v %q", res.err, errs.String())
	}
}
