package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// lockedBuffer is an Out and an Err the question and the test both touch.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// pipeWorld is Lines over an open pipe (the stdin of a command run with no input in an agent's
// shell) whose writer the test holds, on a fake clock, its notice going to errs.
type pipeWorld struct {
	l    *Lines
	w    *os.File
	f    *clock.Fake
	errs *lockedBuffer
}

func piped(t *testing.T, terminal bool) pipeWorld {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pw := pipeWorld{w: w, f: clock.NewFake(0, 0), errs: &lockedBuffer{}}
	pw.l = &Lines{In: r, Out: &lockedBuffer{}, Width: 80, Prog: "tool", Err: pw.errs, Clock: pw.f, IsTerminal: func(any) bool { return terminal }}
	t.Cleanup(func() {
		if err := w.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		if err := pw.l.Close(); err != nil {
			t.Error(err)
		}
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return pw
}

// reply is one question's answer.
type reply struct {
	answer string
	err    error
}

// asked runs Ask in the background.
func (pw pipeWorld) asked() <-chan reply {
	got := make(chan reply, 1)
	go func() {
		answer, err := pw.l.Ask(context.Background(), here, "Name", nil)
		got <- reply{answer, err}
	}()
	return got
}

// due waits until the fake has a timer armed at d, then advances to it.
func (pw pipeWorld) due(t *testing.T, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := pw.f.AwaitDue(ctx, d); err != nil {
		t.Fatal(err)
	}
	pw.f.Advance(d)
}

// An answer piped from an open pipe nothing writes to: the question says on stderr that it is
// waiting, then fails naming the bound, so the command exits non-zero instead of for ever.
func TestAPipedAnswerThatNeverComesFailsNamingTheBound(t *testing.T) {
	pw := piped(t, false)
	got := pw.asked()
	pw.due(t, stdinread.NoticeAfter)
	pw.due(t, stdinread.PipedGiveUp-stdinread.NoticeAfter)
	res := <-got
	var none *stdinread.NoInputError
	if !errors.As(res.err, &none) || !strings.Contains(res.err.Error(), "no input arrived on stdin within 60 s") {
		t.Fatalf("%q %v", res.answer, res.err)
	}
	if !strings.HasPrefix(pw.errs.String(), "tool: waiting for input on stdin") {
		t.Fatalf("stderr %q", pw.errs.String())
	}
}

// The bound is per line: a slow but live script that sends each answer within it is answered,
// here two answers 30 s apart.
func TestSlowPipedAnswersWithinTheBoundAreEachAnswered(t *testing.T) {
	pw := piped(t, false)
	for _, answer := range []string{"first", "second"} {
		got := pw.asked()
		pw.due(t, stdinread.NoticeAfter)
		pw.f.Advance(30*time.Second - stdinread.NoticeAfter)
		if _, err := pw.w.WriteString(answer + "\n"); err != nil {
			t.Fatal(err)
		}
		if res := <-got; res.err != nil || res.answer != answer {
			t.Fatalf("%q %v", res.answer, res.err)
		}
	}
}

// A terminal is a person, who may think as long as they need: no notice, no bound.
func TestATerminalAnswerIsNotBounded(t *testing.T) {
	pw := piped(t, true)
	got := pw.asked()
	pw.f.Advance(10 * stdinread.PipedGiveUp)
	if _, err := pw.w.WriteString("late\n"); err != nil {
		t.Fatal(err)
	}
	if res := <-got; res.err != nil || res.answer != "late" || pw.errs.String() != "" {
		t.Fatalf("%q %v %q", res.answer, res.err, pw.errs.String())
	}
}

// A cancel still ends a piped question as a cancel, with nothing said.
func TestACancelledPipedQuestionIsACancel(t *testing.T) {
	pw := piped(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pw.l.Ask(ctx, here, "Name", nil); !errors.Is(err, ErrCancelled) || pw.errs.String() != "" {
		t.Fatalf("%v %q", err, pw.errs.String())
	}
}
