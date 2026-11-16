package stdinread

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// sink is a destination that hands each write to the test as it is made.
type sink chan string

func (s sink) Write(p []byte) (int, error) {
	s <- string(p)
	return len(p), nil
}

// copied is one Copy's answer.
type copied struct {
	n   int64
	err error
}

// startCopy runs Copy into w in the background; the answer arrives on the channel.
func startCopy(w io.Writer, r io.Reader, o Options) <-chan copied {
	got := make(chan copied, 1)
	go func() {
		n, err := Copy(w, r, o)
		got <- copied{n, err}
	}()
	return got
}

// Copy is live: what has arrived reaches w before the input ends (a sign-in link shown while
// the token it leads to has not come).
func TestCopyStreamsBeforeTheEnd(t *testing.T) {
	r, w := pipe(t)
	out := make(sink, 4)
	got := startCopy(out, r, Options{Err: io.Discard, Clock: clock.NewFake(0, 0), GiveUpAfter: time.Minute, IsTerminal: notTerminal})
	if _, err := w.WriteString("first"); err != nil {
		t.Fatal(err)
	}
	if s := <-out; s != "first" {
		t.Fatalf("first write %q", s)
	}
	if _, err := w.WriteString(" second"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if s := <-out; s != " second" {
		t.Fatalf("second write %q", s)
	}
	if res := <-got; res.err != nil || res.n != int64(len("first second")) {
		t.Fatalf("%d %v", res.n, res.err)
	}
}

// An open pipe nothing writes to: Copy says it is waiting, then gives up at the bound, having
// written nothing.
func TestCopyOfAnOpenPipeIsNoticedThenGivenUp(t *testing.T) {
	f := clock.NewFake(0, 0)
	var errs syncBuffer
	out := make(sink, 1)
	got := startCopy(out, silent(t), Options{Prog: "ccy --secret-screen", Err: &errs, Clock: f, GiveUpAfter: time.Minute, IsTerminal: notTerminal})
	due(t, f, NoticeAfter)
	due(t, f, time.Minute-NoticeAfter)
	res := <-got
	var none *NoInputError
	if !errors.As(res.err, &none) || res.n != 0 || len(out) != 0 {
		t.Fatalf("%d %v", res.n, res.err)
	}
	if !strings.HasPrefix(errs.String(), "ccy --secret-screen: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
}

// Input that has started is copied to its end however slowly it comes.
func TestCopyOfStartedInputIsNotGivenUp(t *testing.T) {
	r, w := pipe(t)
	f := clock.NewFake(0, 0)
	started := make(chan struct{})
	var dst syncBuffer
	got := startCopy(&dst, r, Options{Err: io.Discard, Clock: f, GiveUpAfter: time.Minute, IsTerminal: notTerminal, onStart: func() { close(started) }})
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
	if res := <-got; res.err != nil || dst.String() != "part rest" {
		t.Fatalf("%q %v", dst.String(), res.err)
	}
}

// Copy takes Read's Limit and terminal refusal.
func TestCopyKeepsTheLimitAndTheTerminalRefusal(t *testing.T) {
	var dst syncBuffer
	n, err := Copy(&dst, strings.NewReader("abcdef"), Options{Limit: 4, Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal})
	if err != nil || n != 4 || dst.String() != "abcd" {
		t.Fatalf("%d %v %q", n, err, dst.String())
	}
	terminal := func(any) bool { return true }
	if _, err := Copy(&dst, strings.NewReader("x"), Options{RefuseTerminal: true, Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: terminal}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("%v", err)
	}
}

// A destination that fails ends the copy with its error.
func TestCopyReportsAFailedWrite(t *testing.T) {
	_, err := Copy(failWriter{}, strings.NewReader("x"), Options{Err: io.Discard, Clock: clock.NewFake(0, 0), IsTerminal: notTerminal})
	if err == nil || !strings.Contains(err.Error(), "full") {
		t.Fatalf("%v", err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
