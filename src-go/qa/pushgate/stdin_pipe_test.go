package pushgate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/stdinread/stdinreadtest"
)

// Issue 260: the gate run by hand with an open pipe nothing writes to (an agent's shell's stdin
// for a command given no input) says it is waiting, then cannot run, naming the bound.
func TestTheGateOnAnOpenPipeIsBounded(t *testing.T) {
	r := newRig(t)
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
	done := make(chan ExitCode, 1)
	go func() {
		done <- Main(context.Background(), []string{"origin", r.url}, Env{
			Stdin: pr, Stdout: &r.out, Stderr: &r.err, Runner: r.runner, Cwd: cwd,
			Getenv: func(k string) string { return r.env[k] }, ReadFile: r.readFile, Clock: c,
		})
	}()
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
	for _, want := range []string{"qa-push-gate: waiting for input on stdin", "no input arrived on stdin within 60 s"} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, r.err.String())
		}
	}
}
