package picker

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/hot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The three ways into an add from the teams list (Plan 00203 SPEC-team-add-and-restart.md A1-A15):
// a team's own menu, which asks which running session; a running member's row, whose session is
// known; and a seat row with no running session, whose team and seat are known. Each goes on
// through the Running sessions view's own questions, those already answered left out.
const (
	actAddRunning = "add-running"
	actAddThis    = "add-this"
	actAddToSeat  = "add-to-seat"
)

// addRunningCrumb is the crumb of an add begun from a team's menu.
const addRunningCrumb = "Add a running session"

// addActions are the add lines of row's menu: on a team's row, adding a running session to it;
// on a seat row whose session runs, adding that session to another team; on a seat row of a
// known checkout whose session does not run, adding a running session to that seat.
func (r *run) addActions(row board.Row) []action {
	switch {
	case row.Kind == board.KindTeam:
		return []action{{actAddRunning, "Add a running session to " + row.Team, "add a running session to " + row.Team}}
	case !row.Kind.IsSession():
	case row.Record != "" && r.live(row.Record):
		return []action{{actAddThis, "Add this session to another team", "add its session to another team"}}
	case row.Dir != "-":
		return []action{{actAddToSeat, unavailable("Add a running session to this seat", r.seatTaken(row)), "add a running session to this seat"}}
	}
	return nil
}

// seatTaken is why the seat of row cannot take a running session, "" when it can: the claims
// `session add` refuses (H22), read from the same pass. A stopped session's record claims it
// only when a restore starts that session again.
func (r *run) seatTaken(row board.Row) string {
	switch {
	case row.Held:
		return "another process holds it"
	case row.Record != "" && r.snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]:
		return "session " + row.Record + "'s record starts it again after a reboot (Stop starting its session again after a reboot frees it)"
	}
	return ""
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
	h := flow.Here(team, addRunningCrumb)
	about := "team " + team
	refuse := func(s hot.Session) string {
		if cell, in := cellOf(s, team); in {
			return r.alreadyIn(s, cell)
		}
		if !s.Controllable {
			return cannotChange(s) + "."
		}
		return ""
	}
	s, res, ok, err := r.chooseRunning(ctx, team, team, h.AtStep(1, 3, "Session").About(about), refuse)
	if !ok {
		return res, err
	}
	return r.addSeatAndRole(ctx, s, team, h, about+" · session "+s.Name)
}

// addToSeat is a seat row's Add a running session to this seat: the team and the seat are the
// row's, so only the session and the role are asked. A session is refused as addRunningTo
// refuses it, and also when it runs in another checkout or is of another kind than the seat,
// whose member belongs to one checkout and one kind (A12-A15).
func (r *run) addToSeat(ctx context.Context, row board.Row) (flow.Result, error) {
	k := r.p.Kit
	team, seat := row.Team, "seat "+row.Member
	if why := r.seatTaken(row); why != "" {
		return k.Refuse(ctx, "Seat "+row.Member+" of "+team+" cannot take a running session: "+why+".")
	}
	h := flow.Here(team, row.Crumb(), addRunningCrumb)
	about := "team " + team + " · " + seat
	refuse := func(s hot.Session) string {
		if cell, in := cellOf(s, team); in {
			return r.alreadyIn(s, cell)
		}
		switch {
		case s.Kind != string(row.Kind):
			return s.Name + " is a " + s.Kind + " session, and " + seat + " is a " + string(row.Kind) + " seat."
		case s.Checkout != row.Dir:
			return s.Name + " runs in " + k.Abbrev(s.Checkout) + ", and " + seat + " is a seat of " + k.Abbrev(row.Dir) + "."
		case !s.Controllable:
			return cannotChange(s) + "."
		}
		return ""
	}
	s, res, ok, err := r.chooseRunning(ctx, team, seat+" of "+team, h.AtStep(1, 2, "Session").About(about), refuse)
	if !ok {
		return res, err
	}
	fixed := option{value: row.Member, handle: row.Handle, exists: true}
	return r.addInRole(ctx, s, team, fixed, h.AtStep(2, 2, "Role"), "team "+team+" · session "+s.Name)
}

// chooseRunning is A3-A6 for an add to the place named by into (team, or a seat of it): the
// team refused first when nothing can join it, then every running session, each that refuse
// gives a reason for refused with it; with none that can be chosen nothing is asked.
func (r *run) chooseRunning(ctx context.Context, team, into string, h ui.Heading, refuse func(hot.Session) string) (hot.Session, flow.Result, bool, error) {
	k := r.p.Kit
	stop := func(res flow.Result, err error) (hot.Session, flow.Result, bool, error) {
		return hot.Session{}, res, false, err
	}
	if why := r.cannotBeJoined(team); why != "" {
		return stop(k.Refuse(ctx, why))
	}
	sessions, err := r.p.Session.Sessions(ctx)
	if err != nil {
		return stop(k.Refuse(ctx, "The running sessions cannot be listed: "+err.Error()+"."))
	}
	if len(sessions) == 0 {
		return stop(k.Note(ctx, "No session is running, so none can be added to "+into+". Start a ccy session in "+team+
			", or start one with ccy and add it here."))
	}
	opts := make([]option, len(sessions))
	eligible := 0
	for i, s := range sessions {
		opts[i] = option{line: r.sessionLine(s, sessions), value: s.Name, refused: refuse(s)}
		if opts[i].refused == "" {
			eligible++
		}
	}
	if eligible == 0 {
		why := make([]string, len(opts))
		for i, o := range opts {
			why[i] = "  " + strings.TrimSuffix(o.refused, ".")
		}
		return stop(k.Note(ctx, "No running session can be added to "+into+":\n"+strings.Join(why, "\n")))
	}
	chosen, res, ok, err := r.ask(ctx, h, "Add which running session to "+into+"?", opts)
	if !ok {
		return stop(res, err)
	}
	return sessions[chosen.at], flow.Done, true, nil
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
