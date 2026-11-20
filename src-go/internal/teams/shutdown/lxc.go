package shutdown

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// Why an LXC member is offline, has no session, or cannot be reached.
const (
	whyLxcDeferred = "offline (its container was not running at the last apply, so it has not joined yet)"
	whyNoSudo      = "lxc-attach needs sudo"
	whyUnreachable = "unreachable: "
	whyLxcUnknown  = "no LXC member record of this host names it (a container on another machine, or one " +
		"joined by hand); in its container, type /exit in its session"
	// lxcTmux is the socket and the session of a v1 LXC member's service unit (members.py
	// SERVICE_TMUX).
	lxcTmux = "agent-bus-claude"
)

// Containers is the host's LXC, as lxcfreeze.Lister reads it: where root keeps its containers,
// which run, and a runner for tmux inside one as its user.
type Containers interface {
	LxcPath(ctx context.Context) (string, error)
	Containers(ctx context.Context) (running, stopped []string, err error)
	InContainer(lxcPath, container, user string) execx.Runner
}

// resolveLxc sorts the team's LXC member rows (SPEC-team-close TC2-TC4): one the record says the
// last apply deferred (never joined), or whose container is not running now, is offline, which is
// normal lifecycle and never a failure (CLAUDE/AgentNotes.md, "An offline team member never
// blocks anything"); one whose container runs is closed in its session there, through
// lxc-attach as its user, when it has one, and has nothing to shut down when it has none. A row
// joined, or held back as stopped at the last apply, is judged by the live container list, read
// as root.
func (s *Shutdown) resolveLxc(ctx context.Context, team string, p *plan) {
	rows := p.lxc
	p.lxc = nil
	if len(rows) == 0 {
		return
	}
	rec, _, err := lxcrecord.Read(s.AppliedDir, team)
	if err != nil {
		p.notReached(err.Error(), rows...)
		return
	}
	var items []lxcrecord.Item
	var joined []board.Row
	for _, row := range rows {
		i := slices.IndexFunc(rec.Members, func(it lxcrecord.Item) bool { return it.Handle == row.Handle })
		switch {
		case i < 0:
			p.notReached(whyLxcUnknown, row)
		case rec.Members[i].Status == lxcrecord.StatusDeferred:
			p.offline = append(p.offline, unreached{row.Handle, "offline (container " + rec.Members[i].Container + " stopped at the last apply)"})
		default:
			items, joined = append(items, rec.Members[i]), append(joined, row)
		}
	}
	if len(joined) == 0 {
		return
	}
	if err = s.Sudo.Ready(ctx, "Closing the LXC members' sessions runs lxc-ls and lxc-attach as root."); err != nil {
		p.notReached(whyNoSudo+": "+err.Error(), joined...)
		return
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		p.notReached(whyUnreachable+err.Error(), joined...)
		return
	}
	lxcPath := ""
	for i, row := range joined {
		it := items[i]
		if !slices.Contains(running, it.Container) {
			p.offline = append(p.offline, unreached{row.Handle, "offline (container " + it.Container + " stopped)"})
			continue
		}
		if lxcPath == "" {
			if lxcPath, err = s.Lxc.LxcPath(ctx); err != nil {
				p.notReached(whyUnreachable+err.Error(), joined[i:]...)
				return
			}
		}
		server := &tmuxccy.Tmux{Runner: s.Lxc.InContainer(lxcPath, it.Container, it.User), Term: s.Term, Socket: lxcTmux}
		s.addGuest(ctx, p, guestSeat{
			label: it.Label(), handle: row.Handle, container: it.Container,
			where: "seat " + row.Handle + ", its session " + lxcTmux + " in container " + it.Container,
			guest: &sessclose.Guest{Tmux: server, Session: lxcTmux, Where: "container " + it.Container},
		})
	}
}

// guestSeat is an LXC seat whose container runs: its label, handle and container, the preview's
// words for where it is, and its session there.
type guestSeat struct {
	label, handle, container, where string
	guest                           *sessclose.Guest
}

// addGuest makes a job of an LXC seat whose session runs in its container; one with no session
// there has nothing to shut down (TC4), and a server that cannot be listed is unreachable (TC12).
func (s *Shutdown) addGuest(ctx context.Context, p *plan, g guestSeat) {
	live, err := g.guest.Tmux.List(ctx)
	if err != nil {
		p.unreached = append(p.unreached, unreached{g.handle, whyUnreachable + err.Error()})
		return
	}
	if !slices.ContainsFunc(live, func(sess tmuxccy.Session) bool { return sess.Name == g.guest.Session }) {
		p.idle = append(p.idle, unreached{g.handle, "no session running in container " + g.container})
		return
	}
	p.jobs = append(p.jobs, &job{name: g.label, handle: g.handle, where: g.where, guest: g.guest})
}

// notReached names rows as out of reach, for why.
func (p *plan) notReached(why string, rows ...board.Row) {
	for _, row := range rows {
		p.unreached = append(p.unreached, unreached{row.Handle, why})
	}
}
