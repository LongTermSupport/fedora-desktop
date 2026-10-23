// Package statusline is `teamchat status-line` (Plan 00209 B 2.9 and B27, SPEC-n3 SL1): the
// session's own status line with the team segment under it.
//
// The inner line is the statusLine command the session would run without the kit (Plan 00213
// SPEC-status-line S5-S15 as v1's rules, n3e-D16), run as /bin/sh -c in a session of its own
// with the payload on stdin, killed with its group at 10 s. The team segment is one read of
// status.json and one probe of link.lock (one more read and probe, client.ReadDown, only while
// the link is down): each team in its own colour with this session's short member name and role
// letter, or one red form saying why the link does not answer, and the red notes.
//
// It always exits 0 (OF1): a missing settings file, an unreadable status.json, a team down or a
// link stopped are drawn, never an exit. Its one other exit is a cancel by SIGTERM, SIGINT or
// SIGHUP: 128+n with nothing on stdout. It writes no file.
package statusline

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	// maxPayload bounds the payload read as JSON; a larger one is no payload (S1).
	maxPayload = 1 << 20
	// innerGrace is how long a killed inner command's output may stay open (a process that left
	// its group holding stdout) before the render stops waiting on it.
	innerGrace = time.Second
)

// deps are what the status line waits on, runs through and is cancelled by.
type deps struct {
	clock   clock.Clock
	runner  execx.Runner
	signals <-chan os.Signal
}

// Main is `teamchat status-line`; cmd/teamchat puts it in cli.Env.Verbs as "status-line".
func Main(e cli.Env, _ cli.Invocation) term.ExitCode {
	// Subscribed before stdin is read, so an early cancel is not lost (S12).
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)
	return run(e, deps{clock: clock.System(), runner: execx.OS{Grace: innerGrace}, signals: sigs})
}

// cancelled is the cause of a render a signal ended.
type cancelled syscall.Signal

func (c cancelled) Error() string { return "cancelled by " + syscall.Signal(c).String() }

func run(e cli.Env, d deps) term.ExitCode {
	ctx, cancel := context.WithCancelCause(context.Background())
	// The relay ends with ctx and is waited for, so it never outlives the render.
	var relays sync.WaitGroup
	defer relays.Wait()
	defer cancel(nil)
	relays.Go(func() { relay(ctx, d.signals, cancel) })
	raw, ok := readStdin(ctx, e.Stdin)
	if !ok {
		return cancelExit(ctx)
	}
	inner := ""
	if e.Getenv(paths.EnvStatusLineInner) == "" {
		inner = d.inner(ctx, e, raw)
	}
	if ctx.Err() != nil {
		return cancelExit(ctx)
	}
	text := inner
	if seg := segment(e.Getenv, d.clock); seg != "" {
		if text != "" {
			text += "\n"
		}
		text += seg
	}
	// Nothing to draw is no output at all: a lone newline would draw a blank row.
	if text == "" {
		return term.ExitOK
	}
	if _, err := io.WriteString(e.Stdout, text+"\n"); err != nil {
		return term.ExitOK
	}
	return term.ExitOK
}

// relay turns the first signal into the render's cancel, until the render is over.
func relay(ctx context.Context, signals <-chan os.Signal, cancel context.CancelCauseFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-signals:
			if s, ok := sig.(syscall.Signal); ok {
				cancel(cancelled(s))
				return
			}
		}
	}
}

// cancelExit is 128 plus the signal that cancelled ctx.
func cancelExit(ctx context.Context) term.ExitCode {
	if c, ok := errors.AsType[cancelled](context.Cause(ctx)); ok {
		return term.ExitCode(128 + int(c))
	}
	return term.ExitOK
}

// readStdin is the payload, read beside the cancel so a signal during the read still ends the
// render; a stdin that fails to read, or none, is no payload. False is a cancel first.
func readStdin(ctx context.Context, stdin io.Reader) ([]byte, bool) {
	if stdin == nil {
		return nil, true
	}
	got := make(chan []byte, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(stdin, maxPayload+1))
		if err != nil {
			raw = nil
		}
		got <- raw
	}()
	select {
	case raw := <-got:
		return raw, true
	case <-ctx.Done():
		return nil, false
	}
}
