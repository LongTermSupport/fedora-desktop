package lightning

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Issue 260: a team list that never comes (an open pipe nothing writes to) is said on stderr,
// then given up on at the bound, exit 1, instead of waiting for ever.
func TestATeamListThatNeverComesFailsNamingTheBound(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{w, r} {
			if err := f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		}
	})
	world := newWorld(t)
	var out, errOut bytes.Buffer
	a := world.app([]string{"check"}, "", &out, &errOut)
	f := clock.NewFake(0, 0)
	a.Stdin, a.Clock = r, f
	done := make(chan term.ExitCode, 1)
	go func() { done <- a.Run() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if err := f.AwaitDue(ctx, d); err != nil {
			t.Fatal(err)
		}
		f.Advance(d)
	}
	code := <-done
	if code != term.ExitFailure || !strings.Contains(errOut.String(), "agent-bus-lightning: reading the team list: no input arrived on stdin within 60 s") ||
		!strings.HasPrefix(errOut.String(), "agent-bus-lightning check: waiting for input on stdin") {
		t.Fatalf("%d %q %q", code, out.String(), errOut.String())
	}
}
