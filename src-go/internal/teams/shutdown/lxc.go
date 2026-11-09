package shutdown

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
)

// Why an LXC member is offline or cannot be told apart from an online one.
const (
	whyLxcDeferred = "offline (its container was not running at the last apply, so it has not joined yet)"
	whyNoSudo      = "lxc-ls needs sudo"
)

// Containers is the host's LXC, as lxcfreeze.Lister reads it: which containers run.
type Containers interface {
	Containers(ctx context.Context) (running, stopped []string, err error)
}

// resolveLxc sorts the team's LXC member rows: one the record says the last apply deferred
// (never joined), or whose container is not running now, is offline, which is normal lifecycle
// and never a failure (CLAUDE/AgentNotes.md, "An offline team member never blocks anything");
// one whose container runs is online and out of reach of this host's tmux. A row joined, or
// held back as stopped at the last apply, is judged by the live container list, read as root.
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
			p.notReached(whyLxc, row)
		case rec.Members[i].Status == lxcrecord.StatusDeferred:
			p.offline = append(p.offline, unreached{row.Handle, "offline (container " + rec.Members[i].Container + " stopped at the last apply)"})
		default:
			items, joined = append(items, rec.Members[i]), append(joined, row)
		}
	}
	if len(joined) == 0 {
		return
	}
	if err = s.Sudo.Ready(ctx, "Whether the LXC members' containers run is read with lxc-ls as root."); err != nil {
		p.notReached(whyNoSudo+": "+err.Error(), joined...)
		return
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		p.notReached(err.Error(), joined...)
		return
	}
	for i, row := range joined {
		if slices.Contains(running, items[i].Container) {
			p.notReached(whyLxc, row)
			continue
		}
		p.offline = append(p.offline, unreached{row.Handle, "offline (container " + items[i].Container + " stopped)"})
	}
}

// notReached names rows as out of reach, for why.
func (p *plan) notReached(why string, rows ...board.Row) {
	for _, row := range rows {
		p.unreached = append(p.unreached, unreached{row.Handle, why})
	}
}
