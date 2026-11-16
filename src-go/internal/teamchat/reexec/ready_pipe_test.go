package reexec

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

// lockedBuffer is a stderr the helper and the test both touch, which says on noticed when it
// is first written.
type lockedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	noticed chan struct{}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() == 0 {
		close(b.noticed)
	}
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A held helper waits on its release for as long as its test holds it: a release that has not
// come after a second is said on the helper's stderr, it is never given up on, and the release
// still ends the wait.
func TestAHeldHelperSaysItIsWaitingAndWaitsForItsRelease(t *testing.T) {
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
	clk, errs := clock.NewFake(0, 0), &lockedBuffer{noticed: make(chan struct{})}
	var out bytes.Buffer
	h := &Helper{stdin: r, out: &out, err: errs, clock: clk, pid: 42}
	got := make(chan error, 1)
	go func() { got <- h.Ready() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if derr := clk.AwaitDue(ctx, stdinread.NoticeAfter); derr != nil {
		t.Fatal(derr)
	}
	clk.Advance(10 * stdinread.PipedGiveUp)
	<-errs.noticed
	if _, werr := w.WriteString("go\n"); werr != nil {
		t.Fatal(werr)
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if err = <-got; err != nil || out.String() != "ready 42\n" {
		t.Fatalf("err %v out %q", err, out.String())
	}
	if !strings.HasPrefix(errs.String(), "reexec helper: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
}
