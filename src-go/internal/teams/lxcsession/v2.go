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

// v2Ask is a v2 row whose start line depends on its unit's state: its index, container and unit.
type v2Ask struct {
	i         int
	box, unit string
}

// offersV2 is Offers for v2 teams' LXC seats (SPEC-team-close TC16), each row's answer in out:
// the seat is an entry of the team's LXC record, its container runs, its session unit's file is
// placed (service: true; LoadState not "not-found") and the unit does not run. The containers
// are listed once, the LXC path read once and each container's units asked in one systemctl
// show, as root without a password; what cannot be asked keeps the line, so choosing it says why.
func (s *Starter) offersV2(ctx context.Context, rows []board.Row, out []bool, at []int) {
	var asks []v2Ask
	for k, row := range rows {
		e, seat, found, err := s.v2Seat(row)
		switch {
		case err != nil:
			out[at[k]] = true
		case !found || e.Status == lxcstate.StatusDeferred:
		default:
			asks = append(asks, v2Ask{i: at[k], box: e.Container.String(), unit: v2Unit(seat)})
		}
	}
	if len(asks) == 0 {
		return
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		keepAll(out, asks)
		return
	}
	asks = slices.DeleteFunc(asks, func(a v2Ask) bool { return !slices.Contains(running, a.box) })
	if len(asks) == 0 {
		return
	}
	lxcPath, err := s.Lxc.LxcPath(ctx)
	if err != nil {
		keepAll(out, asks)
		return
	}
	var boxes []string
	for _, a := range asks {
		if !slices.Contains(boxes, a.box) {
			boxes = append(boxes, a.box)
		}
	}
	for _, box := range boxes {
		s.askBox(ctx, lxcPath, box, asks, out)
	}
}

// askBox asks the units of one container's rows in one systemctl show.
func (s *Starter) askBox(ctx context.Context, lxcPath, box string, asks []v2Ask, out []bool) {
	var inBox []v2Ask
	var units []string
	for _, a := range asks {
		if a.box == box {
			inBox, units = append(inBox, a), append(units, a.unit)
		}
	}
	states, err := s.Lxc.UnitStates(ctx, lxcPath, box, units...)
	if err != nil {
		keepAll(out, inBox)
		return
	}
	for j, a := range inBox {
		out[a.i] = states[j].Load != "not-found" && !unitRuns(states[j].Active)
	}
}

// keepAll keeps the start line of every asked row: what could not be asked is said on choosing it.
func keepAll(out []bool, asks []v2Ask) {
	for _, a := range asks {
		out[a.i] = true
	}
}

// startV2 is Start for a v2 team's LXC seat: `teamchat-admin lxc start --member
// <container>/<seat key>` (SU4) as root, the one on-demand start of its session. systemd's start
// leaves a unit that already runs as it is, so a running session is never ended; the check before
// it only says so instead of running anything.
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
	argv := []string{paths.AdminBinaryPath, "lxc", "start", "--member", member}
	if res, err := s.Sudo.Capture(ctx, paths.AdminBinaryPath, "lxc", "start", "--member", member); err != nil {
		return s.Kit.Note(ctx, "Its session was not started: sudo "+strings.Join(argv, " ")+" failed: "+err.Error()+
			": "+strings.TrimSpace(string(res.Stderr)))
	}
	return s.Kit.Note(ctx, "Ran sudo "+strings.Join(argv, " ")+": "+unit+" started in container "+box+
		". It runs until it ends or is stopped; nothing starts it again by itself. Attach: sudo lxc-attach -n "+box+
		" -- runuser -u "+e.User.String()+" -- tmux -S "+paths.GuestTmuxSocket(key)+" attach")
}
