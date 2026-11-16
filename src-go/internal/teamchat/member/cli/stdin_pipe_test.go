package cli

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
	teamclock "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// lockedBuffer is a stream the run and the test both touch.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// silentRun runs args with stdin an open pipe whose writer the test holds (what an agent's shell
// gives a command run with no input) on Env.Clock, a fake advanced past the notice and to the
// bound; it is the exit, stdout and stderr.
func silentRun(t *testing.T, getenv func(string) string, args ...string) (term.ExitCode, string, string) {
	t.Helper()
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
	clk, out, errs := teamclock.NewFake(0, 0), &lockedBuffer{}, &lockedBuffer{}
	got := make(chan term.ExitCode, 1)
	e := Env{Args: args, Stdin: r, Stdout: out, Stderr: errs, Digest: digest, Getenv: getenv, Clock: clk}
	go func() { got <- Run(e) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if derr := clk.AwaitDue(ctx, d); derr != nil {
			t.Fatal(derr)
		}
		clk.Advance(d)
	}
	code := <-got
	return code, out.String(), errs.String()
}

// wantGivenUp holds that the run said it was waiting, then ended with exit and code, its error
// object naming the bound, and printed nothing on stdout.
func wantGivenUp(t *testing.T, verb string, exit term.ExitCode, code string, got term.ExitCode, out, errs string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(errs), "\n")
	e, err := exits.Decode([]byte(lines[len(lines)-1]))
	if got != exit || err != nil || e.Code != code || !strings.Contains(e.Message, "no input arrived on stdin within 60 s") || out != "" {
		t.Fatalf("%s: exit %d stdout %q stderr %q", verb, got, out, errs)
	}
	if !strings.HasPrefix(errs, "teamchat "+verb+": waiting for input on stdin") {
		t.Fatalf("%s: stderr %q", verb, errs)
	}
}

// The guest ops and seat place-issued, given an input that never comes, say they are waiting,
// then fail naming the bound, timed by Env.Clock.
func TestAVerbWhoseInputNeverComesFailsNamingTheBound(t *testing.T) {
	for _, verb := range []string{"membership", "place-bundle"} {
		newGuestWorld(t)
		code, out, errs := silentRun(t, nil, "guest", verb, "--user", "dev", "--key", testKey)
		wantGivenUp(t, "guest "+verb, exits.ExitUsage, exits.CodeUsage, code, out, errs)
	}
	p := newPlaceWorld(t)
	code, out, errs := silentRun(t, func(k string) string { return p.env[k] }, "seat", "place-issued", "--checkout", p.checkout)
	wantGivenUp(t, "seat place-issued", exits.ExitConfig, exits.CodeBundle, code, out, errs)
}
