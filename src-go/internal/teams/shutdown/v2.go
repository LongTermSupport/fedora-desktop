package shutdown

import (
	"context"
	"slices"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// Why a v2 member is out of reach.
const (
	whyV2Elsewhere = "no session on this host's ccy tmux holds it, and it is not a seat of this host's seat store " +
		"(a session on another machine); end it where it runs"
	whyV2NoLxcEntry = "an LXC member no entry of the team's LXC record names, so its container is not known here " +
		"(teamchat-admin lxc apply writes the record)"
	whyV2HeldOffTmux = "which is not a session on this host's ccy tmux (a ccy session started outside tmux " +
		"holds its seats under its container's name); type /exit in it where it runs"
	whyV2HeldUnread = "its seat in this host's seat store is held, but the holding record cannot be read, so " +
		"no session can be named for it; the seat sweep settles it, or end the session where it runs"
)

// resolveV2Lxc sorts a v2 team's LXC seats (SPEC-team-close TC2-TC4): every entry of its LXC
// record that is not left. One the last apply deferred, or whose container is not running now,
// is offline; one whose container runs is closed in its session on the seat's own tmux server
// (`tmux -S`), through lxc-attach as the entry's user (its unit runs as that user), when it has
// one there, and has nothing to shut down when it has none.
func (s *Shutdown) resolveV2Lxc(ctx context.Context, snap *board.Snapshot, team string, p *plan) {
	id, err := paths.ParseTeamID(team)
	if err != nil {
		p.unreached = append(p.unreached, unreached{"the LXC seats of " + team, err.Error()})
		return
	}
	rec, _, err := lxcstate.ReadRecord(s.TeamchatRoot, id)
	if err != nil {
		p.unreached = append(p.unreached, unreached{"the LXC seats of " + team, whyUnreachable + err.Error()})
		return
	}
	p.notReached(whyV2NoLxcEntry, unnamedLxcRows(snap, team, rec)...)
	var live []lxcstate.Entry
	for _, e := range rec.Members {
		switch e.Status {
		case lxcstate.StatusLeft:
		case lxcstate.StatusDeferred:
			p.offline = append(p.offline, unreached{v2Label(e), "offline (deferred at the last apply: " + e.Why + ")"})
		default:
			live = append(live, e)
		}
	}
	if len(live) == 0 {
		return
	}
	labels := make([]string, len(live))
	for i, e := range live {
		labels[i] = v2Label(e)
	}
	if err = s.Sudo.Ready(ctx, "Closing the LXC seats' sessions runs lxc-ls and lxc-attach as root."); err != nil {
		p.notReachedNamed(whyNoSudo+": "+err.Error(), labels...)
		return
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		p.notReachedNamed(whyUnreachable+err.Error(), labels...)
		return
	}
	lxcPath := ""
	for i, e := range live {
		box := e.Container.String()
		if !slices.Contains(running, box) {
			p.offline = append(p.offline, unreached{labels[i], "offline (container " + box + " stopped)"})
			continue
		}
		if lxcPath == "" {
			if lxcPath, err = s.Lxc.LxcPath(ctx); err != nil {
				p.notReachedNamed(whyUnreachable+err.Error(), labels[i:]...)
				return
			}
		}
		session := paths.GuestTmuxSession(id, e.Checkout, e.Seat)
		server := &tmuxccy.Tmux{
			Runner: s.Lxc.InContainer(lxcPath, box, e.User.String()), Term: s.Term,
			SocketPath: paths.GuestTmuxSocket(rec.SeatOf(e).Key()),
		}
		s.addGuest(ctx, p, guestSeat{
			label: session, handle: labels[i], container: box,
			where: "seat " + labels[i] + ", its session " + session + " in container " + box,
			guest: &sessclose.Guest{Tmux: server, Session: session, Where: "container " + box},
		})
	}
}

// unnamedLxcRows is the team's LXC member rows no entry of its LXC record that is not left names
// (TC12): by its handle, or by the handle's seat number for an entry the last apply deferred
// before it had a handle. A missing record names none.
func unnamedLxcRows(snap *board.Snapshot, team string, rec lxcstate.Record) []board.Row {
	var rows []board.Row
	for _, row := range v2Members(snap, team) {
		if row.Kind != kindLxc {
			continue
		}
		h, parsed := proto.ParseHandle(row.Handle)
		named := slices.ContainsFunc(rec.Members, func(e lxcstate.Entry) bool {
			if e.Status == lxcstate.StatusLeft {
				return false
			}
			return e.Handle == row.Handle || e.Handle == "" && parsed && h.Seat == strconv.Itoa(e.Seat)
		})
		if !named {
			rows = append(rows, row)
		}
	}
	return rows
}

// v2Label is how a v2 LXC seat is named: its handle, or where it is while it has none.
func v2Label(e lxcstate.Entry) string {
	if e.Handle != "" {
		return e.Handle
	}
	return e.Container.String() + ":" + e.Checkout + " seat " + strconv.Itoa(e.Seat)
}

// notReachedNamed names members as out of reach, for why.
func (p *plan) notReachedNamed(why string, members ...string) {
	for _, m := range members {
		p.unreached = append(p.unreached, unreached{m, why})
	}
}
