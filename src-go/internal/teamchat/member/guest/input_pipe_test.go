package guest

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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

// lockedBuffer is a stderr the op and the test both touch.
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

// silentInput is an op's stdin as an agent's shell gives a command run with no input: an open
// pipe whose writer the test holds, on a fake clock, its notice going to errs.
type silentInput struct {
	in   Input
	clk  *clock.Fake
	errs *lockedBuffer
}

func newSilentInput(t *testing.T) silentInput {
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
	s := silentInput{clk: clock.NewFake(0, 0), errs: &lockedBuffer{}}
	s.in = Input{Stdin: r, Err: s.errs, Clock: s.clk}
	return s
}

// giveUp advances the fake past the notice, then to the bound, each once its timer is armed.
func (s silentInput) giveUp(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if err := s.clk.AwaitDue(ctx, d); err != nil {
			t.Fatal(err)
		}
		s.clk.Advance(d)
	}
}

// wantGivenUp holds that op said it was waiting, then refused 64 usage naming the bound.
func (s silentInput) wantGivenUp(t *testing.T, verb string, err error) {
	t.Helper()
	wantCode(t, err, 64, exits.CodeUsage)
	if !strings.Contains(err.Error(), "no input arrived on stdin within 60 s") {
		t.Fatalf("%s: %v", verb, err)
	}
	if !strings.HasPrefix(s.errs.String(), "teamchat "+verb+": waiting for input on stdin") {
		t.Fatalf("%s: stderr %q", verb, s.errs.String())
	}
}

// Each op that reads stdin gives up on an open pipe nothing writes to, naming the bound, and
// changes nothing.
func TestAnOpWhoseInputNeverComesFailsNamingTheBound(t *testing.T) {
	// Each op is set up on the test's goroutine and run on its own, its changes recorded in r.
	for _, tc := range []struct {
		verb  string
		setUp func(t *testing.T, r *recorder) func(in Input) error
	}{
		{"guest check", func(t *testing.T, _ *recorder) func(in Input) error {
			t.Helper()
			c, u, team := newGuestWorld(t).checker(), agentUser(t), alpha(t)
			return func(in Input) error { return c.Check(t.Context(), u, team, in) }
		}},
		{"guest place-bundle", func(t *testing.T, r *recorder) func(in Input) error {
			t.Helper()
			w := newWorld(t)
			return func(in Input) error { return PlaceBundle(t.Context(), w.home, w.key, w.clock, in, r) }
		}},
		{"guest membership", func(t *testing.T, r *recorder) func(in Input) error {
			t.Helper()
			w := newWorld(t)
			return func(in Input) error { return Membership(w.home, w.key, in, r) }
		}},
	} {
		s := newSilentInput(t)
		var r recorder
		run := tc.setUp(t, &r)
		got := make(chan error, 1)
		go func() { got <- run(s.in) }()
		s.giveUp(t)
		s.wantGivenUp(t, tc.verb, <-got)
		if len(r.whats) != 0 {
			t.Errorf("%s changed %v", tc.verb, r.whats)
		}
	}
}
