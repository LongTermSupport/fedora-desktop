// Package seats is what the picker does to a team's seats and members: attach the terminal to
// the session holding a seat, start a new ccy session in a team's free or new seat, turn the
// restart of a seat's session after a reboot on or off, and remove a seat, park a member or
// take a team down. It owns those flows; the picker decides when to call them.
package seats

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Seats is the flows on a team's seats. Every dependency is a field so a test stands in for each.
type Seats struct {
	Kit  *flow.Kit
	Ops  *ops.Ops
	Tmux *tmuxccy.Tmux
	// Checkouts says whether the folder ccy-teams runs in is a checkout a session may start in.
	Checkouts *hostcheckout.Finder
	Bus       *agentbus.Client
	Systemd   *systemd.Systemd
	// Registry finds the session registry, which depends on the environment.
	Registry func() (*registry.Store, error)
	// Here is the folder the command was run in.
	Here func() string
	// Exec replaces this process with the launcher, in dir.
	Exec     func(path string, argv []string, dir string) error
	Launcher string
}

// New is the Seats flows over a host.
func New(h *host.Host, kit *flow.Kit, o *ops.Ops) *Seats {
	return &Seats{
		Kit: kit, Ops: o, Tmux: h.Tmux, Checkouts: h.Checkouts, Bus: h.Bus, Systemd: h.Systemd, Registry: h.Registry,
		Here: h.Here, Exec: h.Exec, Launcher: h.Launcher,
	}
}

// StartCcy replaces this process with the launcher, run in top with the given seat refs.
// With no refs it is a bare `ccy --teams`, which lists the teams and lets the person choose.
func (s *Seats) StartCcy(top, refs string) error {
	argv := []string{s.Launcher, ccyenv.FlagTeams}
	said := "Starting ccy --teams"
	if refs != "" {
		argv = append(argv, refs)
		said += " " + refs
	}
	s.Kit.Tell(said + " in " + s.Kit.Abbrev(top) + ".")
	if err := s.Exec(s.Launcher, argv, top); err != nil {
		s.Kit.Term.PrintError(err.Error())
		return flow.Leave(term.ExitFailure)
	}
	return flow.Leave(term.ExitOK)
}

// AttachSeat attaches the terminal to the ccy session holding a seat.
func (s *Seats) AttachSeat(ctx context.Context, snap *board.Snapshot, row board.Row) (flow.Result, error) {
	if row.Dir == "-" {
		return s.Kit.Refuse(ctx, row.Handle+" is a ccy member whose checkout is not known here, so there is no session to attach.")
	}
	if _, live := snap.Live[row.Session]; !live {
		return s.Kit.Refuse(ctx, "Seat "+row.Label()+" in "+row.Team+" has no running session to attach. Ctrl-N starts one.")
	}
	return s.AttachSession(ctx, snap, row.Session)
}

// AttachSession attaches this terminal to session, a session of ccy's tmux, and ends the
// picker; one open in another terminal, or gone, is refused.
func (s *Seats) AttachSession(ctx context.Context, snap *board.Snapshot, session string) (flow.Result, error) {
	clients, live := snap.Live[session]
	if !live {
		return s.Kit.Refuse(ctx, "'"+session+"' is not running in ccy's tmux any more, so there is nothing to attach.")
	}
	if clients != 0 {
		return s.Kit.Refuse(ctx, "'"+session+"' is open in another terminal. ccy-sessions takes it over (Ctrl-T).")
	}
	outcome, err := s.Tmux.Attach(ctx, session)
	if err != nil {
		s.Kit.Term.PrintError(err.Error())
		return s.Kit.Refuse(ctx, "'"+session+"' was taken or ended just now — the list has been refreshed.")
	}
	if outcome == tmuxccy.Kicked {
		s.Kit.BackToList(ctx)
		return flow.Refused, nil
	}
	return flow.Done, flow.Leave(term.ExitOK)
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
			rows, tags = append(rows, agentbus.Label(seat.Handle, "")+"   free"), append(tags, seat.Seat+"@"+team)
		}
	}
	return append(rows, "Seat "+strconv.Itoa(next)+"@"+team+"   new"), append(tags, strconv.Itoa(next)+"@"+team), nil
}

// NewSession starts ccy in this checkout, in a free seat of the team or a new one.
func (s *Seats) NewSession(ctx context.Context, snap *board.Snapshot, team string) (flow.Result, error) {
	here, err := s.Checkouts.Find(ctx, "")
	switch {
	case err != nil:
		return s.Kit.Refuse(ctx, "No session is started: "+err.Error()+".")
	case here.Top == "":
		return s.Kit.Refuse(ctx, s.Kit.Abbrev(s.Here())+" is not a project folder (no git repository), so ccy cannot be started here.")
	case !here.Host():
		return s.Kit.Refuse(ctx, here.Refusal(s.Kit.Abbrev(here.Top)))
	}
	top := here.Top
	state, err := s.Systemd.HomeserverState(ctx, team)
	if err != nil {
		s.Kit.Term.PrintError(err.Error())
		return flow.Refused, flow.Leave(term.ExitFailure)
	}
	if state != systemd.HomeserverUp {
		return s.Kit.Refuse(ctx, team+"'s homeserver is down, so no session can join it.")
	}
	seats, unreadable, err := s.Bus.SeatList(ctx, top)
	if err != nil || unreadable {
		if err != nil && !agentbus.IsCommandError(err) {
			s.Kit.ReportErr(err)
		}
		return s.Kit.Refuse(ctx, "Not every seat of this checkout could be read (above), so no seat is offered: "+
			"one of them may be the seat a new session would take.")
	}
	rows, tags, err := seatChoices(top, team, seats, snap.Members[team])
	if err != nil {
		s.Kit.Term.PrintError(err.Error())
		return flow.Refused, flow.Leave(term.ExitFailure)
	}
	picked, err := s.Kit.Choose(ctx, ui.PickSpec{
		Title: "New ccy session in " + team,
		Header: ui.Header("Enter start   Esc or q back", "Runs ccy --teams <seat>@"+team+" in "+s.Kit.Abbrev(top)+
			"; a new seat is created by that launch (sudo)."),
		Rows: rows, Tags: tags,
	})
	if errors.Is(err, ui.ErrLeave) {
		return flow.Done, s.Kit.Interrupted(ctx)
	}
	if err != nil {
		return flow.Done, err
	}
	if picked.Tag == "" {
		s.Kit.Term.PrintError("the choice '" + picked.Row + "' names no seat.")
		return flow.Refused, flow.Leave(term.ExitFailure)
	}
	return flow.Done, s.StartCcy(top, picked.Tag)
}

// ToggleRestore turns on or off the restart after a reboot of the session holding a seat.
func (s *Seats) ToggleRestore(ctx context.Context, snap *board.Snapshot, row board.Row) (flow.Result, error) {
	current, recorded := snap.SeatRestore[row.Dir+"|"+row.Member+"@"+row.Team]
	if row.Kind != board.KindCcy || !recorded {
		return s.Kit.Note(ctx, "Ctrl-R changes whether a seat's ccy session comes back after a reboot, and this row has no session.")
	}
	name := row.Record
	store, err := s.Registry()
	if err != nil {
		s.Kit.Term.PrintError(err.Error())
		return s.Kit.Note(ctx, "Nothing was changed.")
	}
	want := !current
	if err := store.SetRestore(name, want); err != nil {
		s.Kit.Term.PrintError(err.Error())
		s.Kit.BackToList(ctx)
		return flow.Done, nil
	}
	if want {
		return s.Kit.Note(ctx, "Session "+name+" will be started again after a reboot.")
	}
	return s.Kit.Note(ctx, "Session "+name+" will not be started again after a reboot.")
}

// RemoveOrPark takes down a team, removes a seat, or parks a member, each after asking.
func (s *Seats) RemoveOrPark(ctx context.Context, snap *board.Snapshot, row board.Row) (flow.Result, error) {
	switch {
	case row.Kind == board.KindTeam:
		if err := s.Kit.InPicker("ccy-teams remove "+row.Team, s.Ops.Remove(ctx, []string{row.Team})); err != nil {
			return flow.Done, err
		}
		s.Kit.BackToList(ctx)
		return flow.Done, nil
	case row.Kind == board.KindHuman:
		return s.Kit.Note(ctx, "A team's people are its agent_bus_teams entry's humans: change them there, then ccy-teams apply "+row.Team+".")
	case row.Kind == board.KindCcy && row.Dir != "-":
		return s.removeSeat(ctx, row)
	case row.Kind == board.KindCcy && snap.Incomplete != "":
		return s.Kit.Note(ctx, row.Handle+" may be one of the seats that could not be read (above), so it is not parked here: "+
			"remove it with agent-bus seat remove in its checkout.")
	}
	return s.park(ctx, row)
}

func (s *Seats) removeSeat(ctx context.Context, row board.Row) (flow.Result, error) {
	ref := row.Member + "@" + row.Team
	yes, err := s.Kit.Confirm(ctx, "Remove seat "+ref+"?",
		row.Handle+" is parked (its token revoked; its history kept) and the seat's directory in "+s.Kit.Abbrev(row.Dir)+
			" deleted. Launching into "+ref+" again brings it back.",
		"remove "+ref, false)
	if err != nil || !yes {
		return flow.Done, err
	}
	if err := s.Bus.SeatRemove(ctx, row.Dir, ref); err != nil {
		if !agentbus.IsCommandError(err) {
			s.Kit.ReportErr(err)
		}
		return s.Kit.Note(ctx, "The seat was not removed, for the reason above.")
	}
	s.Kit.BackToList(ctx)
	return flow.Done, nil
}

func (s *Seats) park(ctx context.Context, row board.Row) (flow.Result, error) {
	yes, err := s.Kit.Confirm(ctx, "Park "+row.Handle+"?",
		"Its token is revoked; its account, role and room are kept. add-member --seat returns it, "+
			"and an agent_bus_lxc_members entry is returned by the next play run unless set absent.",
		"park "+row.Handle, false)
	if err != nil || !yes {
		return flow.Done, err
	}
	if !s.Kit.SudoReady(ctx, "A member is parked as root, by agent-bus.") {
		return s.Kit.Note(ctx, row.Handle+" was not parked.")
	}
	if err := s.Bus.ParkMember(ctx, row.Team, row.Handle); err != nil {
		s.Kit.ReportRootErr(err)
		return s.Kit.Note(ctx, row.Handle+" was not parked, for the reason above.")
	}
	s.Kit.BackToList(ctx)
	return flow.Done, nil
}
