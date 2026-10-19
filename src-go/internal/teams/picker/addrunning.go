package picker

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
)

// The two ways into an add from the teams list (Plan 00203 SPEC-team-add-and-restart.md A1-A9):
// a team's own menu, which asks which running session, and a running member's row, whose session
// is known. Both go on through the Running sessions view's own seat and role questions.
const (
	actAddRunning = "add-running"
	actAddThis    = "add-this"
)

// addRunningCrumb is the crumb of an add begun from a team's menu.
const addRunningCrumb = "Add a running session"

// addActions are the add lines of row's menu: on a team's row, adding a running session to it;
// on a seat row whose session runs, adding that session to another team.
func (r *run) addActions(row board.Row) []action {
	switch {
	case row.Kind == board.KindTeam:
		return []action{{actAddRunning, "", "Add a running session to " + row.Team, "add a running session to " + row.Team}}
	case row.Kind == board.KindCcy && row.Record != "" && r.live(row.Record):
		return []action{{actAddThis, "", "Add this session to another team", "add its session to another team"}}
	}
	return nil
}

// live is whether the tmux session name is running.
func (r *run) live(name string) bool {
	_, ok := r.snap.Live[name]
	return ok
}

// alreadyIn is why s cannot be added to the team of cell: it is in it.
func (r *run) alreadyIn(s hot.Session, cell hot.Cell) string {
	return s.Name + " is already in " + cell.Team + " as " + r.seatLabel(s.Checkout, cell) + ": one seat per team per session."
}

// cannotChange is why s cannot be added to any team while it runs.
func cannotChange(s hot.Session) string {
	return s.Name + " cannot be changed while it runs (" + s.State + ")"
}

// addRunningTo is the team menu's Add a running session: every running session, each that
// cannot join team refused with why; then the seat and the role, as the sessions view asks them.
func (r *run) addRunningTo(ctx context.Context, team string) (flow.Result, error) {
	k := r.p.Kit
	sessions, err := r.p.Session.Sessions(ctx)
	if err != nil {
		return k.Refuse(ctx, "The running sessions cannot be listed: "+err.Error()+".")
	}
	if len(sessions) == 0 {
		return k.Note(ctx, "No session is running, so none can be added to "+team+". Start a ccy session in "+team+
			", or start one with ccy and add it here.")
	}
	opts := make([]option, len(sessions))
	eligible := 0
	for i, s := range sessions {
		opts[i] = option{line: r.sessionLine(s, sessions), value: s.Name}
		if cell, in := cellOf(s, team); in {
			opts[i].refused = r.alreadyIn(s, cell)
		} else if !s.Controllable {
			opts[i].refused = cannotChange(s) + "."
		} else {
			eligible++
		}
	}
	if eligible == 0 {
		why := make([]string, len(opts))
		for i, o := range opts {
			why[i] = "  " + strings.TrimSuffix(o.refused, ".")
		}
		return k.Note(ctx, "No running session can be added to "+team+":\n"+strings.Join(why, "\n"))
	}
	h := flow.Here(team, addRunningCrumb)
	chosen, res, ok, err := r.ask(ctx, h.AtStep(1, 3, "Session").About("team "+team), "Add which running session to "+team+"?", opts)
	if !ok {
		return res, err
	}
	s := sessions[chosen.at]
	return r.addSeatAndRole(ctx, s, team, h, "team "+team+" · session "+s.Name)
}

// addThisSession is a seat row's Add this session to another team: the sessions view's Add to a
// team for the session holding the seat.
func (r *run) addThisSession(ctx context.Context, row board.Row) (flow.Result, error) {
	k := r.p.Kit
	sessions, err := r.p.Session.Sessions(ctx)
	if err != nil {
		return k.Refuse(ctx, "The running sessions cannot be listed: "+err.Error()+".")
	}
	for _, s := range sessions {
		if s.Name != row.Record {
			continue
		}
		if !s.Controllable {
			return k.Refuse(ctx, cannotChange(s)+".")
		}
		return r.addToTeam(ctx, s)
	}
	return k.Refuse(ctx, row.Record+" is not among the running sessions ccy-teams can change: it has ended, or it was not started through ccy's tmux.")
}

// sessionLine is a running session as the team menu's add lists it: its name, its checkout and
// each team it is in, by the seat's full handle and the role it was told.
func (r *run) sessionLine(s hot.Session, all []hot.Session) string {
	width := 0
	for _, o := range all {
		width = max(width, len(o.Name))
	}
	in := make([]string, len(s.Teams))
	for i, c := range s.Teams {
		in[i] = "in " + c.Team + " as " + r.seatLabel(s.Checkout, c) + " (" + c.Role + ")"
	}
	teams := "in no team"
	if len(in) > 0 {
		teams = strings.Join(in, " · ")
	}
	checkout := s.Checkout
	if checkout != "-" {
		checkout = r.p.Kit.Abbrev(checkout)
	}
	return s.Name + strings.Repeat(" ", width-len(s.Name)) + "  " + checkout + "  " + teams + "  " + s.State
}
