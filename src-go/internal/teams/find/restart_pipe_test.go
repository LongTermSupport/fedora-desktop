package find

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

// Issue 260: restart-args run with an open pipe nothing writes to (an agent's shell's stdin for
// a command given no input) says it is waiting, then fails naming the bound, writing nothing.
func TestRestartArgsOnAnOpenPipeIsBounded(t *testing.T) {
	h := recordRig(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(w.Close(), r.Close()); err != nil {
			t.Error(err)
		}
	})
	f := clock.NewFake(0, 0)
	h.f.Clock = f
	var out bytes.Buffer
	done := make(chan term.ExitCode, 1)
	go func() { done <- h.f.RestartArgs(context.Background(), []string{"ccy-proj"}, r, &out) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The give-up is armed after the notice, so once it is, both are.
	if err := f.AwaitDue(ctx, stdinread.PipedGiveUp); err != nil {
		t.Fatal(err)
	}
	f.Advance(stdinread.NoticeAfter)
	f.Advance(stdinread.PipedGiveUp - stdinread.NoticeAfter)
	if code := <-done; code != term.ExitFailure || out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
	for _, want := range []string{"ccy-teams internal restart-args: waiting for input on stdin", "no input arrived on stdin within 60 s"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, h.ErrOut.String())
		}
	}
}
