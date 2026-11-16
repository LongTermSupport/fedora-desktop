package statusline

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Claude Code always writes and closes the payload, so the status line never gives up on it;
// but a payload that has not ended after a second is said on stderr, and the line is drawn
// once it ends.
func TestAPayloadThatHasNotEndedIsSaidThenRead(t *testing.T) {
	w := newWorld(t)
	w.env[paths.EnvRuntime] = ""
	w.noInner()
	r, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{pw, r} {
			if cerr := f.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
				t.Error(cerr)
			}
		}
	})
	w.stdin = r
	noticed := w.stderr.await("teamchat status-line: waiting for input on stdin")
	done := w.async()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := w.clk.AwaitDue(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(10 * stdinread.PipedGiveUp)
	<-noticed
	if _, err := pw.WriteString(`{}`); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != term.ExitOK {
		t.Fatalf("exit %d out %q", code, w.out.String())
	}
	if !strings.HasPrefix(w.stderr.String(), "teamchat status-line: waiting for input on stdin") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}
