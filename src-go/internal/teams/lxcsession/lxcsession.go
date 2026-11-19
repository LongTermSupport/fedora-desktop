// Package lxcsession is an LXC member's session started on demand: the menu line Start its
// session in its LXC container, on an LXC member's row. It is the one start of
// play-agent-bus.yml's `service: true` unit (agent-bus-claude-<user>.service, members.py): the
// owner ruled that a member's session never starts with its container (Plan 00257), so the
// unit has no [Install] section, nothing enables it, and the play never starts it.
//
// The row's container and user come from the play's LXC member record of the team
// (lxcrecord); only a member the record holds as joined, or as skipped because its container
// was stopped, is started. Whether the container runs is read with lxc-ls as root, then the
// unit is started inside it as root through lxc-attach (lxcfreeze.Lister.StartUnit). Every
// outcome is said, and the list comes back.
package lxcsession

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
)

// tmuxSocket is the tmux socket and session the unit runs the session on (members.py
// SERVICE_TMUX), for the attach line.
const tmuxSocket = "agent-bus-claude"

// Containers is the host's LXC as the start needs it: where root keeps its containers, which
// run, and a unit started inside one.
type Containers interface {
	LxcPath(ctx context.Context) (string, error)
	Containers(ctx context.Context) (running, stopped []string, err error)
	StartUnit(ctx context.Context, lxcPath, container, unit string) error
}

// Starter starts an LXC member's session. Every dependency is a field so a test stands in for each.
type Starter struct {
	Kit        *flow.Kit
	Lxc        Containers
	Sudo       *sudo.Sudo
	AppliedDir string
}

// New is the Starter over a host.
func New(h *host.Host, kit *flow.Kit) *Starter {
	return &Starter{
		Kit: kit, Lxc: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath}, Sudo: h.Sudo, AppliedDir: h.AppliedDir,
	}
}

// Unit is a member's session unit in its container, as members.py's service_unit names it.
func Unit(user string) string { return "agent-bus-claude-" + user + ".service" }

// Start is Start its session in its LXC container, for row.
func (s *Starter) Start(ctx context.Context, row board.Row) (flow.Result, error) {
	rec, _, err := lxcrecord.Read(s.AppliedDir, row.Team)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	i := slices.IndexFunc(rec.Members, func(it lxcrecord.Item) bool {
		return it.Handle == row.Handle && (it.Status == lxcrecord.StatusJoined || it.Status == lxcrecord.StatusStopped)
	})
	if row.Handle == "" || i < 0 {
		return s.Kit.Note(ctx, "Nothing was started: no LXC member record of this host names "+row.Handle+
			" as a member of "+row.Team+", so its container and user are not known here (play-agent-bus.yml writes the record when it joins the member).")
	}
	it := rec.Members[i]
	if err = s.Sudo.Ready(ctx, "Starting an LXC member's session runs lxc-ls and lxc-attach as root."); err != nil {
		return s.Kit.Note(ctx, "Nothing was started: lxc-attach needs sudo ("+err.Error()+").")
	}
	running, _, err := s.Lxc.Containers(ctx)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	if !slices.Contains(running, it.Container) {
		return s.Kit.Note(ctx, "Nothing was started: container "+it.Container+" is not running.")
	}
	lxcPath, err := s.Lxc.LxcPath(ctx)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	unit := Unit(it.User)
	if err = s.Lxc.StartUnit(ctx, lxcPath, it.Container, unit); err != nil {
		return s.Kit.Note(ctx, "Its session was not started: systemctl start "+unit+" in container "+it.Container+
			" failed: "+err.Error()+". The unit exists only for a member with service: true in agent_bus_lxc_members, once play-agent-bus.yml has run.")
	}
	return s.Kit.Note(ctx, "Started "+unit+" in container "+it.Container+" ("+it.Label()+"). It runs until it ends or is stopped; "+
		"nothing starts it again by itself. Attach: sudo lxc-attach -n "+it.Container+" -- runuser -u "+it.User+
		" -- tmux -L "+tmuxSocket+" attach")
}
