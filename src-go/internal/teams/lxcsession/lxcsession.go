// Package lxcsession is an LXC member's session started on demand: the menu line Start its
// session in its LXC container, on an LXC member's row. It is the one start of
// play-agent-bus.yml's `service: true` unit (agent-bus-claude-<user>.service, members.py): the
// owner ruled that a member's session never starts with its container (Plan 00257), so the
// unit has no [Install] section, nothing enables it, and the play never starts it.
//
// The row's container and user come from the play's LXC member record of the team
// (lxcrecord); only a member the record holds as joined, or as skipped because its container
// was stopped, is started. The line is offered (Offers) only on such a member of this host
// whose agent_bus_lxc_members entry has service: true, the one kind with a unit. Whether the container runs is read with lxc-ls as root, then the
// unit is started inside it as root through lxc-attach (lxcfreeze.Lister.StartUnit). Every
// outcome is said, and the list comes back.
//
// A v2 (teamchat) team's LXC seat (a row board reads from its status record) is the seat of its
// LXC record (lxcstate) holding the row's handle. Its line is offered where the start can work
// (SPEC-team-close TC16): its container runs, its session unit's file is placed (service: true)
// and the unit does not run, asked as root without a password (what cannot be asked keeps the
// line). The start is `teamchat-admin lxc start --member <container>/<seat key>` (SU4) as root.
package lxcsession

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
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
	UnitState(ctx context.Context, lxcPath, container, unit string) (load, active string, err error)
}

// Declared is the host_vars' agent_bus_lxc_members entries, as the play reads them.
type Declared interface {
	LxcMembers(ctx context.Context) ([]declare.LxcMember, error)
}

// Starter starts an LXC member's session. Every dependency is a field so a test stands in for each.
type Starter struct {
	Kit        *flow.Kit
	Lxc        Containers
	Sudo       *sudo.Sudo
	Declare    Declared
	AppliedDir string
	// TeamchatRoot is the root a v2 team's LXC record is read under ("" on a host).
	TeamchatRoot string
}

// New is the Starter over a host.
func New(h *host.Host, kit *flow.Kit) *Starter {
	return &Starter{
		Kit: kit, Lxc: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath}, Sudo: h.Sudo, Declare: h.Declare,
		AppliedDir: h.AppliedDir, TeamchatRoot: h.TeamchatRoot,
	}
}

// recorded is the index of row's member in rec, held as joined or stopped; -1 when none is.
func recorded(rec lxcrecord.Record, row board.Row) int {
	if row.Handle == "" {
		return -1
	}
	return slices.IndexFunc(rec.Members, func(it lxcrecord.Item) bool {
		return it.Handle == row.Handle && (it.Status == lxcrecord.StatusJoined || it.Status == lxcrecord.StatusStopped)
	})
}

// Offers is whether row's menu has Start its session in its LXC container: a member of this
// host (its record holds it as joined or stopped) whose present entry has service: true.
// Another host's member is in no record here, and a service: false member has no unit. A
// record or declaration that cannot be read keeps the line, so choosing it says why.
func (s *Starter) Offers(ctx context.Context, row board.Row) bool {
	if row.V2 {
		return s.offersV2(ctx, row)
	}
	rec, _, err := lxcrecord.Read(s.AppliedDir, row.Team)
	if err != nil {
		return true
	}
	i := recorded(rec, row)
	if i < 0 {
		return false
	}
	entries, err := s.Declare.LxcMembers(ctx)
	if err != nil {
		return true
	}
	it := rec.Members[i]
	return slices.ContainsFunc(entries, func(m declare.LxcMember) bool {
		return m.Team == row.Team && m.Container == it.Container && m.User == it.User && m.State == "present" && m.Service
	})
}

// Unit is a member's session unit in its container, as members.py's service_unit names it.
func Unit(user string) string { return "agent-bus-claude-" + user + ".service" }

// Start is Start its session in its LXC container, for row.
func (s *Starter) Start(ctx context.Context, row board.Row) (flow.Result, error) {
	if row.V2 {
		return s.startV2(ctx, row)
	}
	rec, _, err := lxcrecord.Read(s.AppliedDir, row.Team)
	if err != nil {
		return s.Kit.Note(ctx, "Nothing was started: "+err.Error())
	}
	i := recorded(rec, row)
	if i < 0 {
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
