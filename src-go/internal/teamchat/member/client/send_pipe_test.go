package client

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// openPipe is the stdin of a command run with no input in an agent's shell: a pipe whose
// writer the test holds open, both ends closed at the test's end.
type openPipe struct{ r, w *os.File }

func newOpenPipe(t *testing.T) openPipe {
	t.Helper()
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
	return openPipe{r: r, w: w}
}

// due waits until the world's fake clock has a timer armed at d, then advances to it.
func (w *world) due(d time.Duration) {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := w.clk.AwaitDue(ctx, d); err != nil {
		w.t.Fatal(err)
	}
	w.clk.Advance(d)
}

// A send verb whose text never comes on an open pipe says it is waiting, then exits 64 naming
// the bound, having sent nothing (no link listens, so a connection would have been 69).
func TestASendWhoseTextNeverComesFailsNamingTheBound(t *testing.T) {
	for _, verb := range [][]string{{"say", "--to", "bob"}, {"reply", eventID}, {"report"}} {
		w := newWorld(t)
		w.stdin = newOpenPipe(t).r
		got := w.async(verb...)
		w.due(stdinread.NoticeAfter)
		w.due(stdinread.PipedGiveUp - stdinread.NoticeAfter)
		if code := <-got; code != exits.ExitUsage {
			t.Fatalf("%v: exit %d %q", verb, code, w.stderr.String())
		}
		if e := w.errorObject(); e.Code != exits.CodeUsage || !strings.Contains(e.Message, "no input arrived on stdin within 60 s") {
			t.Fatalf("%v: error %+v", verb, e)
		}
		if !strings.HasPrefix(w.stderr.String(), "teamchat "+verb[0]+": waiting for input on stdin") {
			t.Fatalf("%v: stderr %q", verb, w.stderr.String())
		}
	}
}

// Text that comes slowly, within the bound, is sent: the bound is on silence.
func TestASendWhoseTextComesWithinTheBoundIsSent(t *testing.T) {
	w := newWorld(t)
	p := newOpenPipe(t)
	w.stdin = p.r
	l := w.listen(doneOK)
	got := w.async("say", "--to", "bob")
	w.due(stdinread.NoticeAfter)
	w.clk.Advance(30 * time.Second)
	if _, err := p.w.WriteString("late but here\n"); err != nil {
		t.Fatal(err)
	}
	if err := p.w.Close(); err != nil {
		t.Fatal(err)
	}
	if code := <-got; code != term.ExitOK {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	if req := l.request(t); req.Stdin == nil || *req.Stdin != "late but here" {
		t.Fatalf("stdin %v", req.Stdin)
	}
}
