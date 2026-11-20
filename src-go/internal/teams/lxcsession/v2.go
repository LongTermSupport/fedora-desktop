package lxcsession

import (
	"context"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
)

// v2Seat is a v2 row's seat: its LXC record entry and its identity; found is false when no
// entry that is not left holds the row's handle.
func (s *Starter) v2Seat(row board.Row) (lxcstate.Entry, lxcstate.SeatID, bool, error) {
	id, err := paths.ParseTeamID(row.Team)
	if err != nil {
		return lxcstate.Entry{}, lxcstate.SeatID{}, false, err
	}
	rec, _, err := lxcstate.ReadRecord(s.TeamchatRoot, id)
	if err != nil {
		return lxcstate.Entry{}, lxcstate.SeatID{}, false, err
	}
	e, ok := rec.ByHandle(row.Handle)
	if !ok {
		return lxcstate.Entry{}, lxcstate.SeatID{}, false, nil
	}
	return e, rec.SeatOf(e), true, nil
}

// v2Unit is the seat's session unit in its container.
func v2Unit(seat lxcstate.SeatID) string {
	return paths.SessionUnit(seat.Team, seat.Checkout, seat.Seat)
}

// unitRuns is whether systemd's ActiveState is a unit's running (or starting) one.
func unitRuns(active string) bool {
	return active == systemd.ActiveStateActive || slices.Contains([]string{"activating", "reloading", "refreshing"}, active)
}

// offersV2 is Offers for a v2 team's LXC seat (SPEC-team-close TC16): the seat is an entry of
// the team's LXC record, its container runs, its session unit's file is placed (service: true;
// LoadState not "not-found") and the unit does not run. The containers and the unit are asked
// as root without a password; what cannot be asked keeps the line, so choosing it says why.
func (s *Starter) offersV2(ctx context.Context, row board.Row) bool {
	e, seat, found, err := s.v2Seat(row)
	if err != nil {
		return true
	}
	if !found || e.Status == lxcstate.StatusDeferred {
		return false
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		return true
	}
	box := e.Container.String()
	if !slices.Contains(running, box) {
		return false
	}
	lxcPath, err := s.Lxc.LxcPath(ctx)
	if err != nil {
		return true
	}
	load, active, err := s.Lxc.UnitState(ctx, lxcPath, box, v2Unit(seat))
	if err != nil {
		return true
	}
	return load != "not-found" && !unitRuns(active)
}

// startV2 is Start for a v2 team's LXC seat: `teamchat-admin lxc restart --member
// <container>/<seat key>` (SU3) as root, the one on-demand start of its session; a unit that
// already runs is not restarted, since a restart would end the session.
func (s *Starter) startV2(ctx context.Context, row board.Row) (flow.Result, error) {
	e, seat, found, err := s.v2Seat(row)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	if !found {
		return s.Kit.Note(ctx, "Nothing was started: the LXC record of "+row.Team+" names no seat "+row.Handle+
			" (teamchat-admin lxc apply writes it).")
	}
	box := e.Container.String()
	if err = s.Sudo.Ready(ctx, "Starting an LXC seat's session runs lxc-ls, lxc-attach and teamchat-admin as root."); err != nil {
		return s.Kit.Note(ctx, "Nothing was started: it needs sudo ("+err.Error()+").")
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	if !slices.Contains(running, box) {
		return s.Kit.Note(ctx, "Nothing was started: container "+box+" is not running.")
	}
	lxcPath, err := s.Lxc.LxcPath(ctx)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	unit := v2Unit(seat)
	load, active, err := s.Lxc.UnitState(ctx, lxcPath, box, unit)
	switch {
	case err != nil:
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	case load == "not-found":
		return s.Kit.Note(ctx, "Nothing was started: container "+box+" has no unit "+unit+
			" (the seat has service: false, or no lxc apply has placed it).")
	case unitRuns(active):
		return s.Kit.Note(ctx, "Nothing was started: its session already runs ("+unit+" is "+active+" in container "+box+").")
	}
	key := seat.Key()
	member := box + "/" + key.String()
	argv := []string{paths.AdminBinaryPath, "lxc", "restart", "--member", member}
	if res, err := s.Sudo.Capture(ctx, paths.AdminBinaryPath, "lxc", "restart", "--member", member); err != nil {
		return s.Kit.Note(ctx, "Its session was not started: sudo "+strings.Join(argv, " ")+" failed: "+err.Error()+
			": "+strings.TrimSpace(string(res.Stderr)))
	}
	return s.Kit.Note(ctx, "Ran sudo "+strings.Join(argv, " ")+": "+unit+" started in container "+box+
		". It runs until it ends or is stopped; nothing starts it again by itself. Attach: sudo lxc-attach -n "+box+
		" -- runuser -u "+e.User.String()+" -- tmux -S "+paths.GuestTmuxSocket(key)+" attach")
}
