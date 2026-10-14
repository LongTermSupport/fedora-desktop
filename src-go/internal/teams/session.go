package teams

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// refuse says why a key could do nothing, and waits for the list to be read before it is drawn
// over the message. It counts toward the limit of refusals in a row.
func (r *pickerRun) refuse(ctx context.Context, msg string) (result, error) {
	r.a.tell(msg)
	r.a.backToList(ctx)
	return refused, nil
}

// note says something the person should read before the list is drawn again; it is not a
// refusal.
func (r *pickerRun) note(ctx context.Context, msg string) (result, error) {
	r.a.tell(msg)
	r.a.backToList(ctx)
	return done, nil
}

// reportRootErr says why a root command failed when its own words have not already been shown:
// sudo wanting a password again prints nothing.
func (a *App) reportRootErr(err error) {
	switch {
	case errors.Is(err, sudo.ErrNeedsPassword):
		a.Term.PrintError("sudo asked for a password again, so nothing was changed. Run sudo -v first.")
	case !isCommandErr(err):
		a.reportErr(err)
	}
}

// attachSeat attaches the terminal to the ccy session holding a seat.
func (r *pickerRun) attachSeat(ctx context.Context, row Row) (result, error) {
	a := r.a
	if row.Dir == "-" {
		return r.refuse(ctx, row.Handle+" is a ccy member whose checkout is not known here, so there is no session to attach.")
	}
	clients, live := r.snap.Live[row.Session]
	if !live {
		return r.refuse(ctx, "Seat "+row.Member+"@"+row.Team+" has no running session to attach. Ctrl-N starts one.")
	}
	if clients != 0 {
		return r.refuse(ctx, "'"+row.Session+"' is open in another terminal. ccy-sessions takes it over (Ctrl-T).")
	}
	outcome, err := a.Tmux.Attach(ctx, row.Session)
	if err != nil {
		a.Term.PrintError(err.Error())
		return r.refuse(ctx, "'"+row.Session+"' was taken or ended just now — the list has been refreshed.")
	}
	if outcome == tmuxccy.Kicked {
		a.backToList(ctx)
		return refused, nil
	}
	return done, leave(term.ExitOK)
}

// numericSeat is the number of a seat named by digits only.
func numericSeat(seat string) (int, bool) {
	n, err := strconv.Atoi(seat)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == seat
}

// nextSeat is one past the highest numbered seat the team has in this checkout, or that a
// member of this repository holds: another repository's seats do not take numbers here.
func nextSeat(top string, own []agentbus.Seat, team string, roster agentbus.Roster) (int, error) {
	podman, err := regexp.Compile(`^` + regexp.QuoteMeta(filepath.Base(top)) + `\.([0-9]+)\+[^+]*\.podman$`)
	if err != nil {
		return 0, fmt.Errorf("match the members of %s: %w", filepath.Base(top), err)
	}
	highest := 0
	for _, seat := range own {
		if n, ok := numericSeat(seat.Seat); ok && seat.Team == team {
			highest = max(highest, n)
		}
	}
	for _, m := range roster.Members {
		if found := podman.FindStringSubmatch(m.Handle); found != nil {
			if n, ok := numericSeat(found[1]); ok {
				highest = max(highest, n)
			}
		}
	}
	return highest + 1, nil
}

// seatChoices are the rows of the new-session picker, each tagged with its seat: every free seat
// of the team in this checkout, then a new one.
func seatChoices(top, team string, seats []agentbus.Seat, roster agentbus.Roster) ([]string, []string, error) {
	next, err := nextSeat(top, seats, team, roster)
	if err != nil {
		return nil, nil, err
	}
	var rows, tags []string
	for _, seat := range seats {
		if seat.Team == team && !seat.Held {
			rows, tags = append(rows, "Seat "+seat.Seat+"@"+team+"   free"), append(tags, seat.Seat+"@"+team)
		}
	}
	return append(rows, "Seat "+strconv.Itoa(next)+"@"+team+"   new"), append(tags, strconv.Itoa(next)+"@"+team), nil
}

// newSession starts ccy in this checkout, in a free seat of the team or a new one.
func (r *pickerRun) newSession(ctx context.Context, team string) (result, error) {
	a := r.a
	project, err := a.Git.ProjectHere(ctx, "")
	if err != nil {
		a.reportErr(err)
		return refused, leave(term.ExitFailure)
	}
	if !project {
		return r.refuse(ctx, a.abbrev(a.here())+" is not a project folder (no git repository), so ccy cannot be started here.")
	}
	state, err := a.homeserverState(ctx, team)
	if err != nil {
		a.Term.PrintError(err.Error())
		return refused, leave(term.ExitFailure)
	}
	if state != systemd.HomeserverUp {
		return r.refuse(ctx, team+"'s homeserver is down, so no session can join it.")
	}
	top, err := a.Git.TopLevel(ctx, "")
	if err != nil {
		a.reportErr(err)
		return refused, leave(term.ExitFailure)
	}
	seats, unreadable, err := a.Bus.SeatList(ctx, top)
	if err != nil || unreadable {
		if err != nil && !isCommandErr(err) {
			a.reportErr(err)
		}
		return r.refuse(ctx, "Not every seat of this checkout could be read (above), so no seat is offered: "+
			"one of them may be the seat a new session would take.")
	}
	rows, tags, err := seatChoices(top, team, seats, r.snap.Members[team])
	if err != nil {
		a.Term.PrintError(err.Error())
		return refused, leave(term.ExitFailure)
	}
	picked, err := a.choose(ctx, ui.PickSpec{
		Title: "New ccy session in " + team,
		Header: ui.Header("Enter start   Esc or q back", "Runs ccy --teams <seat>@"+team+" in "+a.abbrev(top)+
			"; a new seat is created by that launch (sudo)."),
		Rows: rows, Tags: tags,
	})
	if errors.Is(err, ui.ErrLeave) {
		return done, a.interrupted(ctx)
	}
	if err != nil {
		return done, err
	}
	if picked.Tag == "" {
		a.Term.PrintError("the choice '" + picked.Row + "' names no seat.")
		return refused, leave(term.ExitFailure)
	}
	return done, a.startCcy(top, picked.Tag)
}

// toggleRestore turns on or off the restart after a reboot of the session holding a seat.
func (r *pickerRun) toggleRestore(ctx context.Context, row Row) (result, error) {
	a := r.a
	current, recorded := r.snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]
	if row.Kind != kindCcy || !recorded {
		return r.note(ctx, "Ctrl-R changes whether a seat's ccy session comes back after a reboot, and this row has no session.")
	}
	name := row.Record
	store, err := a.Registry()
	if err != nil {
		a.Term.PrintError(err.Error())
		return r.note(ctx, "Nothing was changed.")
	}
	want := !current
	if err := store.SetRestore(name, want); err != nil {
		a.Term.PrintError(err.Error())
		a.backToList(ctx)
		return done, nil
	}
	if want {
		return r.note(ctx, "Session "+name+" will be started again after a reboot.")
	}
	return r.note(ctx, "Session "+name+" will not be started again after a reboot.")
}

// removeOrPark takes down a team, removes a seat, or parks a member, each after asking.
func (r *pickerRun) removeOrPark(ctx context.Context, row Row) (result, error) {
	a := r.a
	switch {
	case row.Kind == kindTeam:
		if err := a.inPicker("ccy-teams remove "+row.Team, a.Ops.remove(ctx, []string{row.Team})); err != nil {
			return done, err
		}
		a.backToList(ctx)
		return done, nil
	case row.Kind == kindHuman:
		return r.note(ctx, "A team's people are its agent_bus_teams entry's humans: change them there, then ccy-teams apply "+row.Team+".")
	case row.Kind == kindCcy && row.Dir != "-":
		return r.removeSeat(ctx, row)
	case row.Kind == kindCcy && r.snap.Incomplete != "":
		return r.note(ctx, row.Handle+" may be one of the seats that could not be read (above), so it is not parked here: "+
			"remove it with agent-bus seat remove in its checkout.")
	}
	return r.park(ctx, row)
}

func (r *pickerRun) removeSeat(ctx context.Context, row Row) (result, error) {
	a := r.a
	ref := row.Member + "@" + row.Team
	yes, err := a.confirm(ctx, "Remove seat "+ref+"?",
		row.Handle+" is parked (its token revoked; its history kept) and the seat's directory in "+a.abbrev(row.Dir)+
			" deleted. Launching into "+ref+" again brings it back.",
		"remove "+ref, false)
	if err != nil || !yes {
		return done, err
	}
	if err := a.Bus.SeatRemove(ctx, row.Dir, ref); err != nil {
		if !isCommandErr(err) {
			a.reportErr(err)
		}
		return r.note(ctx, "The seat was not removed, for the reason above.")
	}
	a.backToList(ctx)
	return done, nil
}

func (r *pickerRun) park(ctx context.Context, row Row) (result, error) {
	a := r.a
	yes, err := a.confirm(ctx, "Park "+row.Handle+"?",
		"Its token is revoked; its account, role and room are kept. add-member --seat returns it, "+
			"and an agent_bus_lxc_members entry is returned by the next play run unless set absent.",
		"park "+row.Handle, false)
	if err != nil || !yes {
		return done, err
	}
	if !a.sudoReady(ctx, "A member is parked as root, by agent-bus.") {
		return r.note(ctx, row.Handle+" was not parked.")
	}
	if err := a.Bus.ParkMember(ctx, row.Team, row.Handle); err != nil {
		a.reportRootErr(err)
		return r.note(ctx, row.Handle+" was not parked, for the reason above.")
	}
	a.backToList(ctx)
	return done, nil
}
