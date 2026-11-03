package ccylaunch

import (
	"context"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// stop.go is F23, `ccy --stop SESSION... [--no-compact]`: each named session on ccy's tmux
// server closed as a person would, through sessclose (Plan 00191 Task 4.12).

const (
	stopFlag      = "--stop"
	noCompactFlag = "--no-compact"
)

// stopSessions is the flag: every name checked against the tmux server first, so a mistyped
// name closes nothing, then all of them closed together, one line each on stderr.
func (f Front) stopSessions(ctx context.Context) term.ExitCode {
	var names []string
	noCompact := false
	for _, a := range f.Args[1:] {
		switch {
		case a == noCompactFlag:
			noCompact = true
		case strings.HasPrefix(a, "-"):
			return f.failWith(term.ExitUsage, "ccy: --stop does not take '"+a+"'. Usage: ccy --stop SESSION... [--no-compact]")
		default:
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return f.failWith(term.ExitUsage, "ccy: --stop needs the name of a session (ccy-sessions lists them). Usage: ccy --stop SESSION... [--no-compact]")
	}
	list, err := f.tmux().List(ctx)
	if err != nil {
		return f.fail("ccy: " + err.Error() + ". Nothing was closed.")
	}
	for _, n := range names {
		if !slices.ContainsFunc(list, func(s tmuxccy.Session) bool { return s.Name == n }) {
			return f.fail("ccy: there is no session " + n + " on ccy's tmux server; ccy-sessions lists them. Nothing was closed.")
		}
	}
	closer := f.closer()
	targets := make([]sessclose.Target, len(names))
	for i, n := range names {
		targets[i] = sessclose.Target{Name: n, NoCompact: noCompact}
	}
	var p term.Printer
	if !noCompact {
		p.Print(f.Stderr, "Each session is compacted (/compact), then sent /exit; a compaction is waited for, "+
			sessclose.Seconds(closer.Limits().Compact)+" at most.\n")
	}
	code := term.ExitOK
	for _, r := range closer.Close(ctx, targets) {
		line, ok := resultLine(r)
		if !ok {
			code = term.ExitFailure
		}
		p.Print(f.Stderr, line+"\n")
	}
	if p.Err() != nil {
		return term.ExitFailure
	}
	return code
}

// resultLine is what one session's close did, and whether it ended.
func resultLine(r sessclose.Result) (string, bool) {
	switch r.Outcome {
	case sessclose.Exited:
		if r.Note != "" {
			return r.Name + ": exited cleanly; " + r.Note, true
		}
		return r.Name + ": exited cleanly", true
	case sessclose.Stopped:
		return r.Name + ": stopped: " + r.Note, true
	case sessclose.Pending, sessclose.Failed:
	}
	return r.Name + ": failed: " + r.Note, false
}

// tmux is ccy's tmux server, through the front's runner.
func (f Front) tmux() *tmuxccy.Tmux {
	return &tmuxccy.Tmux{Runner: f.Runner, Term: &term.Term{Err: f.Stderr, IsTerminal: f.IsTerminal, Getenv: f.getenv}}
}

// closer is what --stop closes through: sessclose over the front's runner, clock and pause,
// unless a test gave another.
func (f Front) closer() sessclose.Closing {
	if f.Closer != nil {
		return f.Closer
	}
	c := sessclose.New(f.tmux(), func() (*registry.Store, error) { return registry.Locate(f.getenv) }, f.getenv)
	c.Now, c.Sleep = f.clock().Now, f.pause
	return c
}
