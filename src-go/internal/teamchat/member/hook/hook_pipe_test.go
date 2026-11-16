package hook

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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// noticeWriter is a stderr the hook and the test both touch, which says on noticed when it is
// first written.
type noticeWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	noticed chan struct{}
}

func (n *noticeWriter) Write(p []byte) (int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.buf.Len() == 0 {
		close(n.noticed)
	}
	return n.buf.Write(p)
}

func (n *noticeWriter) String() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.buf.String()
}

// Claude Code always writes and closes the payload, so a hook never gives up on it; but a
// payload that has not ended after a second is said on stderr, and the hook answers once it
// ends.
func TestAPayloadThatHasNotEndedIsSaidThenRead(t *testing.T) {
	w := newWorld(t)
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
	inv, err := cli.Parse([]string{"hook", "stop"})
	if err != nil {
		t.Fatal(err)
	}
	stderr := &noticeWriter{noticed: make(chan struct{})}
	e := cli.Env{Stdin: r, Stdout: w, Stderr: stderr, Digest: digest, Getenv: func(k string) string { return w.env[k] }}
	exit := make(chan term.ExitCode, 1)
	go func() { exit <- run(e, inv, w.deps()) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := w.clk.AwaitDue(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	w.clk.Advance(10 * stdinread.PipedGiveUp)
	<-stderr.noticed
	if _, err := pw.WriteString(`{}`); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	if code := <-exit; code != term.ExitOK || !strings.HasSuffix(w.stdout.String(), "\n") {
		t.Fatalf("exit %d stdout %q", code, w.stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "teamchat hook: waiting for input on stdin") {
		t.Fatalf("stderr %q", stderr.String())
	}
}
