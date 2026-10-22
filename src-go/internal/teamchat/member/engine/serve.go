package engine

import (
	"context"
	"errors"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// history's --limit (n3c-D8): at most one /messages page.
const maxHistory = 100

const (
	msgNotHeld   exits.Template = "this session holds no team {}; run status for its teams"
	msgUnbuilt   exits.Template = "the link does not serve {} yet"
	msgLimit     exits.Template = "history --limit is 1 to 100"
	msgNoTeamNow exits.Template = "team {} is held but its team goroutine could not start; run status"
)

// Serve answers one request (B 2.4.4) with its records into out and its done: recv, wait,
// inbox, status, verify, history, say and ping; every other op is internal, "not served yet". A recv or
// wait that handed items out answers with an ack, which the link turns into Ack or Return.
// The request's deadline is ctx's.
func (e *Engine) Serve(ctx context.Context, req wire.Request, out RecordWriter) wire.Done {
	switch req.Op {
	case wire.OpRecv, wire.OpInbox, wire.OpStatus:
		var a wire.TeamsArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		switch req.Op {
		case wire.OpRecv:
			return e.recv(ctx, a.Team, out)
		case wire.OpInbox:
			return e.inbox(ctx, a.Team, out)
		}
		return e.status(ctx, a.Team, out)
	case wire.OpWait:
		var a wire.WaitArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		return e.wait(ctx, a.Team, out)
	case wire.OpVerify:
		var a wire.VerifyArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		return e.verify(ctx, a, out)
	case wire.OpHistory:
		var a wire.HistoryArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		return e.history(ctx, a, out)
	case wire.OpSay:
		var a wire.SayArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		return e.say(ctx, a, req.Stdin, out)
	case wire.OpPing:
		var a wire.PingArgs
		if err := wire.DecodeArgs(req.Args, &a); err != nil {
			return doneOf(err)
		}
		return e.ping(ctx, a, req.Stdin, out)
	}
	return doneOf(exits.New(exits.CodeInternal, nil, msgUnbuilt, exits.Name(req.Op)))
}

// doneOf is err's done: its own exit and object, or internal.
func doneOf(err error) wire.Done {
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		return wire.Done{Exit: int(xe.Exit), Error: xe}
	}
	return internal(err)
}

// internal is a failure inside the link. Its message is a fixed template, which never quotes
// err; err itself is in link.log when the caller logged it.
func internal(error) wire.Done {
	return wire.Done{Exit: int(exits.ExitInternal), Error: exits.New(exits.CodeInternal, nil, msgInternal)}
}

const msgInternal exits.Template = "the link failed inside; run status, and tell a human if it goes on"

func notHeld(t paths.TeamID) wire.Done {
	return doneOf(exits.New(exits.CodeUsage, &t, msgNotHeld, exits.Team(t)))
}

// single is a single-team verb's team (CL6): the flag, else the team whose store knows the
// event id, else the only held team; and that team's goroutine.
func (e *Engine) single(ctx context.Context, flag *paths.TeamID, event string) (paths.TeamID, team.Team, error) {
	var (
		active []cli.ActiveTeam
		ledger *paths.TeamID
		teams  = map[paths.TeamID]team.Team{}
	)
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			h := e.teams[id]
			if h.removed {
				continue
			}
			active = append(active, cli.ActiveTeam{ID: id})
			teams[id] = h.team
			if event != "" && ledger == nil && h.store != nil && knows(h.store, event) {
				ledger = &id
			}
		}
	}); err != nil {
		return paths.TeamID{}, nil, err
	}
	t, err := cli.ResolveTeam(flag, ledger, active)
	if err != nil {
		return paths.TeamID{}, nil, err
	}
	tm, ok := teams[t]
	switch {
	case !ok:
		return paths.TeamID{}, nil, exits.New(exits.CodeUsage, &t, msgNotHeld, exits.Team(t))
	case tm == nil:
		return paths.TeamID{}, nil, exits.New(exits.CodeInternal, &t, msgNoTeamNow, exits.Team(t))
	}
	return t, tm, nil
}

func knows(s *state.Store, event string) bool {
	return slices.ContainsFunc(s.Ledger, func(l state.Ledger) bool { return l.ID == event }) ||
		slices.ContainsFunc(s.Pending, func(p state.Pending) bool { return p.ID == event })
}

// verify is LN6's link half: the team goroutine re-fetches and judges the event.
func (e *Engine) verify(ctx context.Context, a wire.VerifyArgs, out RecordWriter) wire.Done {
	t, tm, err := e.single(ctx, a.Team, a.ID)
	if err != nil {
		return doneOf(err)
	}
	v, err := tm.Verify(ctx, a.ID)
	if err != nil {
		return teamDone(t, err)
	}
	if err := out.Record(v); err != nil {
		return internal(err)
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

// history reads the room backwards and consumes nothing.
func (e *Engine) history(ctx context.Context, a wire.HistoryArgs, out RecordWriter) wire.Done {
	if a.Limit < 1 || a.Limit > maxHistory {
		return doneOf(exits.New(exits.CodeUsage, a.Team, msgLimit))
	}
	t, tm, err := e.single(ctx, a.Team, "")
	if err != nil {
		return doneOf(err)
	}
	hs, err := tm.History(ctx, a.Room, a.Limit)
	if err != nil {
		return teamDone(t, err)
	}
	for _, h := range hs {
		if err := out.Record(h); err != nil {
			return internal(err)
		}
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

// teamDone is a team call's failure: its own error object, or a context's end as timed-out.
func teamDone(t paths.TeamID, err error) wire.Done {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return doneOf(exits.New(exits.CodeTimedOut, &t, msgTimedOut, exits.Team(t)))
	}
	return doneOf(err)
}

const msgTimedOut exits.Template = "team {} did not answer within the request's deadline"

var _ records.Record = records.Verified{}
