package shutdown

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// whyV2Host is a v2 member that is not an LXC seat: ccy-teams does not find its session.
const whyV2Host = "a v2 (teamchat) member outside an LXC container: ccy-teams does not find its session; " +
	"type /exit in it where it runs"

// planForV2 is a v2 team's members that are not LXC seats, out of reach (TC12); its LXC seats
// are read from its LXC record (resolveV2Lxc), and a parked or retired member has no session.
func planForV2(snap *board.Snapshot, team string) plan {
	var p plan
	for _, row := range snap.Rows {
		parked := strings.HasPrefix(row.State, "parked") || strings.HasPrefix(row.State, "retired")
		if row.Team != team || !row.V2 || row.Kind == board.KindTeam || row.Kind == kindLxc || parked {
			continue
		}
		p.unreached = append(p.unreached, unreached{row.Handle, whyV2Host})
	}
	return p
}

// resolveV2Lxc sorts a v2 team's LXC seats (SPEC-team-close TC2-TC4): every entry of its LXC
// record that is not left. One the last apply deferred, or whose container is not running now,
// is offline; one whose container runs is closed in its session on the seat's own tmux server
// (`tmux -S`), through lxc-attach as the entry's user (its unit runs as that user), when it has
// one there, and has nothing to shut down when it has none.
func (s *Shutdown) resolveV2Lxc(ctx context.Context, team string, p *plan) {
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
