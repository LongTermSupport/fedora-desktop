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

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread/stdinreadtest"
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

// armed waits until the read's give-up hold is armed, which it is after the notice's.
func armed(t *testing.T, c *stdinreadtest.Clock, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := c.Await(ctx, d); err != nil {
		t.Fatal(err)
	}
}

func TestInputThatIsThereIsReadAndNothingIsSaid(t *testing.T) {
	var errs syncBuffer
	data, err := Read(strings.NewReader("hello\n"), Options{Prog: "tool", Err: &errs, GiveUpAfter: PipedGiveUp, Clock: stdinreadtest.NewClock()})
	if err != nil || string(data) != "hello\n" || errs.String() != "" {
		t.Fatalf("%q %v %q", data, err, errs.String())
	}
}

func TestAtMostTheLimitIsRead(t *testing.T) {
	data, err := Read(strings.NewReader("abcdef"), Options{Limit: 4, Err: io.Discard, Clock: stdinreadtest.NewClock()})
	if err != nil || string(data) != "abcd" {
		t.Fatalf("%q %v", data, err)
	}
}

// An open pipe nothing writes to: the notice after NoticeAfter, then the refusal at the bound,
// which names it.
func TestAnOpenPipeIsNoticedThenGivenUpOn(t *testing.T) {
	r, w := pipe(t)
	if w == nil {
		t.Fatal("no writer")
	}
	c := stdinreadtest.NewClock()
	var errs syncBuffer
	got := start(r, Options{Prog: "tool", Err: &errs, GiveUpAfter: PipedGiveUp, Clock: c})
	armed(t, c, NoticeAfter)
	c.Fire(NoticeAfter)
	c.Fire(PipedGiveUp)
	res := <-got
	var none *NoInputError
	if !errors.As(res.err, &none) || res.err.Error() != "no input arrived on stdin within 60 s" {
		t.Fatalf("%q %v", res.data, res.err)
	}
	if !strings.HasPrefix(errs.String(), "tool: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
}

// The bound firing before the notice (a clock that passes both at once) still gives the notice
// first: the refusal never comes without it.
func TestABoundBeforeTheNoticeStillGivesTheNoticeFirst(t *testing.T) {
	r, w := pipe(t)
	if w == nil {
		t.Fatal("no writer")
	}
	c := stdinreadtest.NewClock()
	var errs syncBuffer
	got := start(r, Options{Prog: "tool", Err: &errs, GiveUpAfter: PipedGiveUp, Clock: c})
	armed(t, c, NoticeAfter)
	c.Fire(PipedGiveUp)
	res := <-got
	var none *NoInputError
	if !errors.As(res.err, &none) || !strings.HasPrefix(errs.String(), "tool: waiting for input on stdin") {
		t.Fatalf("%v stderr %q", res.err, errs.String())
	}
}

// With no bound the notice comes and the read waits on: input that then arrives is read whole.
func TestWithNoBoundTheNoticeComesAndTheReadWaitsOn(t *testing.T) {
	r, w := pipe(t)
	c := stdinreadtest.NewClock()
	var errs syncBuffer
	got := start(r, Options{Err: &errs, Clock: c})
	armed(t, c, NoticeAfter)
	c.Fire(NoticeAfter)
	if _, err := w.WriteString("late"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	res := <-got
	if res.err != nil || string(res.data) != "late" || !strings.HasPrefix(errs.String(), "waiting for input on stdin") {
		t.Fatalf("%q %v %q", res.data, res.err, errs.String())
	}
}

// Input that has started is read to its end, however long the rest takes.
func TestInputThatHasStartedIsReadToItsEnd(t *testing.T) {
	r, w := pipe(t)
	c := stdinreadtest.NewClock()
	if _, err := w.WriteString("first "); err != nil {
		t.Fatal(err)
	}
	got := start(r, Options{Err: io.Discard, GiveUpAfter: PipedGiveUp, Clock: c})
	if _, err := w.WriteString("second"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if res := <-got; res.err != nil || string(res.data) != "first second" {
		t.Fatalf("%q %v", res.data, res.err)
	}
}

// A read that fails is the error.
func TestAFailedReadIsTheError(t *testing.T) {
	_, err := Read(failing{}, Options{Err: io.Discard, Clock: stdinreadtest.NewClock()})
	if !errors.Is(err, errBroken) {
		t.Fatalf("%v", err)
	}
}

var errBroken = errors.New("broken")

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errBroken }
