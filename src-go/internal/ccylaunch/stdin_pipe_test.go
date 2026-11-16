package ccylaunch

import (
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

// openPipe is the stdin of a command run with no input in an agent's shell: a pipe whose writer
// the test holds open until it ends, so nothing ends the input.
func openPipe(t *testing.T) *os.File {
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
	return r
}

// startPiped runs the front with an open pipe as stdin on a fake clock, advances the clock to the
// notice and then to the bound, and returns the exit code.
func startPiped(t *testing.T, r *run) term.ExitCode {
	t.Helper()
	f := clock.NewFake(0, 0)
	r.front.Stdin, r.front.InputClock = openPipe(t), f
	done := make(chan term.ExitCode, 1)
	go func() { done <- r.front.Run(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if err := f.AwaitDue(ctx, d); err != nil {
			t.Fatal(err)
		}
		f.Advance(d)
	}
	return <-done
}

// Issue 260: a --build-lock answer that never comes on a pipe is said on stderr, then given up
// on at the bound; nothing is stopped.
func TestBuildLockGivesUpOnAPipedAnswerThatNeverComes(t *testing.T) {
	r := liveBuild(t, "")
	r.run.front.IsTerminal = func(any) bool { return false }
	code := startPiped(t, r.run)
	if code != term.ExitFailure || len(r.sent) != 0 {
		t.Fatalf("exit %d, signals %q, stderr %q", code, r.sent, r.stderr())
	}
	mustContain(t, "stderr", r.stderr(), "ccy --build-lock: waiting for input on stdin", "no input arrived on stdin within 60 s")
}

// Issue 260: the Enter that clears the exported token, piped from a pipe nothing writes to, is
// given up on at the bound, and the screen is left all the same.
func TestExportTokenGivesUpOnAPipedEnterThatNeverComes(t *testing.T) {
	r := repoRun(t, []string{"work.2030-06-01.token"}, "--export-token", "work")
	stdout := r.stdout
	r.front.IsTerminal = func(v any) bool { return v == stdout }
	code := startPiped(t, r)
	out := r.stdout.String()
	if code != term.ExitFailure || !strings.Contains(out, term.AltScreenOn) || !strings.HasSuffix(out, term.AltScreenOff) {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	mustContain(t, "stderr", r.stderr.String(), "ccy --export-token: waiting for input on stdin", "no input arrived on stdin within 60 s")
}

// Issue 260: --secret-screen's text, piped from a source that first waits on a password (sudo
// grep, a vault decrypt), is never given up on: only the notice is armed (stdinread's Copy tests
// prove it is said), and text that comes long after is shown and cleared on Enter.
func TestSecretScreenSaysItIsWaitingButWaitsForALateText(t *testing.T) {
	r := screenRun(t, "", true, strings.NewReader("\n"))
	stdout := r.stdout
	r.front.IsTerminal = func(v any) bool { return v == stdout }
	f := clock.NewFake(0, 0)
	stdin, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Error(err)
		}
	})
	r.front.Stdin, r.front.InputClock = stdin, f
	done := make(chan term.ExitCode, 1)
	go func() { done <- r.front.Run(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := f.AwaitDue(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	if armed, err := f.Armed(stdinread.PipedGiveUp); err != nil || armed {
		t.Fatalf("a bound is armed: %v %v", armed, err)
	}
	f.Advance(stdinread.NoticeAfter)
	f.Advance(10 * stdinread.PipedGiveUp)
	if _, err := typed.WriteString("the value\n"); err != nil {
		t.Fatal(err)
	}
	if err := typed.Close(); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != term.ExitOK || !strings.Contains(r.stdout.String(), "the value\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
}

// A piped end of input clears the exported token as an Enter does.
func TestExportTokenClearsOnTheEndOfInput(t *testing.T) {
	r := repoRun(t, []string{"work.2030-06-01.token"}, "--export-token", "work")
	stdout := r.stdout
	r.front.IsTerminal = func(v any) bool { return v == stdout }
	r.front.Stdin, r.front.InputClock = strings.NewReader(""), clock.NewFake(0, 0)
	r.answered(t, term.ExitOK)
	if !strings.HasSuffix(r.stdout.String(), exportClearLine+term.AltScreenOff) {
		t.Fatalf("stdout %q", r.stdout)
	}
}
