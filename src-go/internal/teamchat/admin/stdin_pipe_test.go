package admin

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

// runPiped runs teamchat-admin with e (its Stdin an open pipe) on a fake clock, advances the
// clock to the notice and then to the bound, and returns what the run did.
func runPiped(t *testing.T, e Env) outcome {
	t.Helper()
	f := clock.NewFake(0, 0)
	var stdout, stderr bytes.Buffer
	e.Stdout, e.Stderr, e.Clock = &stdout, &stderr, f
	if e.Digest == "" {
		e.Digest = digest
	}
	done := make(chan term.ExitCode, 1)
	go func() { done <- Run(context.Background(), e) }()
	due(t, f, stdinread.NoticeAfter)
	due(t, f, stdinread.PipedGiveUp-stdinread.NoticeAfter)
	code := <-done
	return outcome{code, stdout.String(), stderr.String()}
}

// due waits until the fake has a timer armed at d, then advances to it.
func due(t *testing.T, f *clock.Fake, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := f.AwaitDue(ctx, d); err != nil {
		t.Fatal(err)
	}
	f.Advance(d)
}

const noInput = "no input arrived on stdin within 60 s"

func saidWaiting(t *testing.T, out outcome, prog string) {
	t.Helper()
	if !strings.Contains(out.stderr, prog+": waiting for input on stdin") {
		t.Errorf("no notice from %s on stderr: %q", prog, out.stderr)
	}
}

// Issue 260: root's child given a pipe nothing writes the request to answers a failure naming
// the bound instead of waiting for ever.
func TestCtlChildGivesUpOnAnOpenPipe(t *testing.T) {
	out := runPiped(t, Env{Args: []string{"ctl-child"}, Stdin: openPipe(t), Proc: childProc()})
	if out.code != term.ExitFailure || !strings.Contains(out.stdout, "cannot read the request: "+noInput) {
		t.Fatalf("%+v", out)
	}
	saidWaiting(t, out, "teamchat-admin ctl-child")
}

// Issue 260: `team approve` and `team apply` with nothing on stdin fail naming the bound.
func TestTeamInputGivesUpOnAnOpenPipe(t *testing.T) {
	for verb, args := range map[string][]string{
		"approve": {"team", "approve", "--team", "alpha"},
		"apply":   {"team", "apply"},
	} {
		t.Run(verb, func(t *testing.T) {
			w := newWorld(t)
			out := runPiped(t, Env{Args: args, Stdin: openPipe(t), Proc: rootProc(), Sys: w.sys()})
			if out.code != term.ExitFailure || !strings.Contains(out.stderr, noInput) {
				t.Fatalf("%+v", out)
			}
			saidWaiting(t, out, "teamchat-admin team "+verb)
		})
	}
}

// Issue 260: `lxc apply` with no declaration on stdin fails naming the bound.
func TestLXCApplyGivesUpOnAnOpenPipe(t *testing.T) {
	w := newWorld(t)
	out := runPiped(t, Env{Args: []string{"lxc", "apply", "--phase", "present"}, Stdin: openPipe(t), Proc: rootProc(), Sys: w.sys()})
	if out.code != term.ExitFailure || !strings.Contains(out.stderr, "cannot read the declaration: "+noInput) {
		t.Fatalf("%+v", out)
	}
	saidWaiting(t, out, "teamchat-admin lxc apply")
}

// onlyWritersAreTerminals is a terminal stdout (the tests' buffer) over a piped stdin.
func onlyWritersAreTerminals(v any) bool {
	_, isStdout := v.(*bytes.Buffer)
	return isStdout
}

// Issue 260: the Enter that clears a secret screen, from a pipe nothing writes to, is given up
// on at the bound, and the screen is left all the same.
func TestAPipedEnterThatNeverComesStillLeavesTheScreen(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	out := runPiped(t, Env{Args: addArgs("3", "password"), Stdin: openPipe(t), Proc: rootProc(), Sys: w.sys(), IsTerminal: onlyWritersAreTerminals})
	if out.code != term.ExitFailure || !strings.HasPrefix(out.stdout, term.AltScreenOn) || !strings.HasSuffix(out.stdout, term.AltScreenOff) || !strings.Contains(out.stderr, noInput) {
		t.Fatalf("%+v", out)
	}
	saidWaiting(t, out, "teamchat-admin")
}

// A piped Enter, and a piped end of input, clear the screen as they always did.
func TestAPipedEnterClearsTheScreen(t *testing.T) {
	for name, stdin := range map[string]string{"an Enter": "\n", "the end of input": ""} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.applied(t)
			var stdout, stderr bytes.Buffer
			code := Run(t.Context(), Env{
				Args: addArgs("3", "password"), Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
				Proc: rootProc(), Digest: digest, Sys: w.sys(), IsTerminal: onlyWritersAreTerminals, Clock: clock.NewFake(0, 0),
			})
			if code != term.ExitOK || !strings.HasSuffix(stdout.String(), issuedClearPrompt+term.AltScreenOff) || stderr.String() != "" {
				t.Fatalf("%d %q %q", code, stdout.String(), stderr.String())
			}
		})
	}
}
