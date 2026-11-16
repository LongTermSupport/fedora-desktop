package element

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

// lockedBuffer is a stderr the run and the test both touch.
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

// EL1: an input that never comes on an open pipe is said on stderr, then refused naming the
// bound, by check and by apply, and nothing is written.
func TestAnInputThatNeverComesIsRefusedNamingTheBound(t *testing.T) {
	for name, run := range map[string]func(Env, *os.File) error{
		"check": func(e Env, r *os.File) error { return Check(e, r) },
		"apply": func(e Env, r *os.File) error { return Apply(e, r) },
	} {
		s := newSetup(t)
		clk, errs := clock.NewFake(0, 0), &lockedBuffer{}
		s.env.Clock, s.env.Stderr = clk, errs
		r := openPipe(t)
		got := make(chan error, 1)
		go func() { got <- run(s.env, r) }()
		giveUp(t, clk)
		err := <-got
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "no input arrived on stdin within 60 s") {
			t.Fatalf("%s: err %v", name, err)
		}
		if !strings.HasPrefix(errs.String(), "teamchat-admin element: waiting for input on stdin") {
			t.Fatalf("%s: stderr %q", name, errs.String())
		}
		if s.stdout.Len() != 0 {
			t.Fatalf("%s: stdout %q", name, s.stdout.String())
		}
	}
}

// openPipe is the read end of a pipe whose writer the test holds open, both ends closed at the
// test's end.
func openPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{w, r} {
			if cerr := f.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
				t.Error(cerr)
			}
		}
	})
	return r
}

// giveUp advances clk past the notice, then to the bound, each once its timer is armed.
func giveUp(t *testing.T, clk *clock.Fake) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if err := clk.AwaitDue(ctx, d); err != nil {
			t.Fatal(err)
		}
		clk.Advance(d)
	}
}
