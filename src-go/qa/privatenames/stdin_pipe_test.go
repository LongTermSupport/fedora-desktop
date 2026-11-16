package privatenames

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread/stdinreadtest"
)

// Issue 260: push run by hand with an open pipe nothing writes to (an agent's shell's stdin for
// a command given no input) says it is waiting, then cannot run, naming the bound.
func TestPushOnAnOpenPipeIsBounded(t *testing.T) {
	r := newRig(true, map[string]proc.Result{"pingbus status": {Stdout: status}}, files{}, "")
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(pw.Close(), pr.Close()); err != nil {
			t.Error(err)
		}
	})
	c := stdinreadtest.NewClock()
	r.env.Stdin, r.env.Clock = pr, c
	done := make(chan ExitCode, 1)
	go func() { done <- r.run("push", "origin", "url") }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := c.Await(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	c.Fire(stdinread.NoticeAfter)
	c.Fire(stdinread.PipedGiveUp)
	if code := <-done; code != ExitCannotRun {
		t.Fatalf("exit %d: %s", code, r.text())
	}
	for _, want := range []string{"qa-private-names push: waiting for input on stdin", "no input arrived on stdin within 60 s"} {
		if !strings.Contains(r.errOut.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, r.errOut.String())
		}
	}
}
