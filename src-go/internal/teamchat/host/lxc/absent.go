package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	memberstate "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The absent phase's whys.
const (
	whyPending   = "guest files pending"
	whyHeldBy    = "seat still held by run "
	whyHeld      = "seat still held; token revoked"
	whyRevoked   = "; token revoked"
	lineRemoved  = "parked and removed"
	lineLeft     = "removed"
	lineNoHandle = "entry removed; it never joined"
)

// only keeps the entries the declaration gives state.
func (r *run) only(state declare.State) {
	r.members = slices.DeleteFunc(r.members, func(m *member) bool { return m.decl.State != state })
}

// absent is C 3.9.2's absent phase (LX8): each member whose entry says absent, or that the
// record names and no entry declares, leaves under its team's lock: park first, then the
// desired file without the seat, the unit gone when the user holds no team, the L3 wait, and
// `guest remove`. A container that is not running is parked from the host and never waited for.
func (r *run) absent(ctx context.Context) {
	r.only(declare.StateAbsent)
	r.undeclared()
	for _, t := range r.teams {
		var leavers []*member
		for _, m := range r.members {
			if m.team.ID == t.ID {
				leavers = append(leavers, m)
			}
		}
		if len(leavers) == 0 {
			continue
		}
		if err := r.leaveTeam(ctx, t.ID, leavers); err != nil {
			r.errs = append(r.errs, err)
			return
		}
	}
}

// undeclared adds each record entry no entry of its team declares.
func (r *run) undeclared() {
	for _, t := range r.teams {
		declared := map[Target]bool{}
		for _, m := range r.req.Decl.LXC {
			if m.Team == t.ID {
				if c, err := paths.ParseContainerName(m.Container); err == nil {
					declared[Target{Container: c, User: m.User}] = true
				}
			}
		}
		for _, e := range r.records[t.ID].Members {
			tg := Target{Container: e.Container, User: e.User}
			if declared[tg] || r.req.Members != nil && !slices.Contains(r.req.Members, tg) {
				continue
			}
			r.members = append(r.members, &member{
				decl:  declare.LXCMember{Team: t.ID, Container: e.Container.String(), User: e.User, State: declare.StateAbsent},
				team:  t, box: e.Container, prior: e, handle: e.Handle, target: r.req.Members != nil,
			})
		}
	}
}

func (r *run) leaveTeam(ctx context.Context, team paths.TeamID, leavers []*member) error {
	lock, err := teamlock.Take(ctx, r.d.LockDir, team, lockVerb, host.PlayWait, r.d.Clock)
	if err != nil {
		return &host.Error{Verb: verb, Step: host.StepLock, Err: err}
	}
	for _, m := range leavers {
		r.leave(ctx, m)
	}
	return lock.Release()
}

// leave is one member's steps 1-5.
func (r *run) leave(ctx context.Context, m *member) {
	parked := false
	if m.handle != "" && m.prior.Status != lxcstate.StatusLeft {
		if err := r.d.Roster.Park(ctx, m.team.ID, m.handle); err != nil {
			r.fail(m, &host.Error{Verb: verb, Step: "park", Err: err})
			return
		}
		parked = true
		r.changed[m.team.ID] = true
	}
	if m.handle == "" {
		m.done, m.status = true, statusDrop
		r.line(m, Changed, lineNoHandle)
		return
	}
	if err := r.dropSeat(m); err != nil {
		r.fail(m, err)
		return
	}
	g, why := r.leaveBox(ctx, m.box)
	if why != "" {
		r.leftOffline(m, why, parked)
		return
	}
	r.leaveGuest(ctx, g, m, parked)
}

// dropSeat is step 2's host half: the host copy without the team's seat and with its farewell,
// written first.
func (r *run) dropSeat(m *member) error {
	h, ok := proto.ParseHandle(m.handle)
	if !ok {
		return &host.Error{Verb: verb, Step: "membership", Err: fmt.Errorf("the record's handle for %s@%s is not a handle", m.decl.User, m.box)}
	}
	cp, _, err := lxcstate.ReadCopy(r.d.Root, m.box, m.decl.User)
	if err != nil {
		return &host.Error{Verb: verb, Step: "membership", Err: err}
	}
	next, changed := cp.Next(without(cp, m.team.ID, h.Seat))
	if changed {
		if err := lxcstate.WriteCopy(r.d.Root, m.box, m.decl.User, next); err != nil {
			return &host.Error{Verb: verb, Step: "membership", Err: err}
		}
	}
	return nil
}

// without is the copy's seats less team's, and its farewells with team's parked one.
func without(cp lxcstate.Copy, team paths.TeamID, seat string) ([]memberstate.DesiredSeat, []memberstate.Farewell) {
	seats := slices.DeleteFunc(slices.Clone(cp.Seats), func(s memberstate.DesiredSeat) bool { return s.Team == team })
	farewell := slices.DeleteFunc(slices.Clone(cp.Farewell), func(f memberstate.Farewell) bool { return f.Team == team })
	return seats, append(farewell, memberstate.Farewell{Team: team, Seat: seat, Reason: memberstate.FarewellParked})
}

// leaveBox is the member's container, pinned once per phase, or why it is offline.
func (r *run) leaveBox(ctx context.Context, c paths.ContainerName) (*guestBox, string) {
	for _, g := range r.boxes {
		if g.name == c {
			if g.gone {
				return g, g.why()
			}
			return g, ""
		}
	}
	g := &guestBox{name: c}
	r.boxes = append(r.boxes, g)
	names, err := r.d.LXC.Containers(ctx)
	switch {
	case err == nil && !slices.Contains(names, c.String()):
		g.gone, g.missing = true, true
	case err != nil:
		g.gone = true
	default:
		if st, err := r.d.LXC.State(ctx, c.String()); err != nil || st != lxccmd.Running {
			g.gone = true
		} else if g.root, err = r.d.Pin(ctx, c.String()); err != nil {
			g.gone = true
		}
	}
	if g.gone {
		return g, g.why()
	}
	return g, ""
}

// why is a gone box's offline cause.
func (g *guestBox) why() string {
	if g.missing {
		return whyMissing
	}
	return whyStopped
}

// leftOffline is a leaving member whose container is not running: parked from the host, its
// entry kept left with the guest files pending, or dropped for a missing container (n3-D12).
func (r *run) leftOffline(m *member, why string, parked bool) {
	m.done, m.status, m.why = true, lxcstate.StatusLeft, why+"; "+whyPending
	switch {
	case why == whyMissing:
		m.status = statusDrop
		r.line(m, Changed, "parked; entry dropped ("+why+")")
	case parked:
		r.line(m, Changed, "parked; "+whyPending+" ("+why+")")
	default:
		r.line(m, Skipped, m.why)
	}
	if m.target {
		r.errs = append(r.errs, &OfflineError{Container: m.box, Why: why})
	}
}

// leaveGuest is steps 2-5 in a running guest.
func (r *run) leaveGuest(ctx context.Context, g *guestBox, m *member, parked bool) {
	passwd, err := readGuest(g.root, guestPasswd, guestfacts.MaxPasswd)
	if err == nil {
		m.acct, err = guestfacts.Passwd(passwd, m.decl.User, r.d.HostUIDs)
	}
	if err != nil {
		r.leaveFailed(ctx, g, m, "reads", err)
		return
	}
	cp, _, err := lxcstate.ReadCopy(r.d.Root, m.box, m.decl.User)
	if err == nil {
		err = r.desired(ctx, m, cp)
	}
	if err == nil && len(cp.Seats) == 0 {
		if err = r.removeUnit(ctx, g, m.decl.User); unitDown(err) {
			err = nil
		}
	}
	if err != nil {
		r.leaveFailed(ctx, g, m, "membership", err)
		return
	}
	held, run, stopped := r.waitFree(ctx, g, m)
	switch {
	case stopped:
		r.leftOffline(m, whyStopped, false)
		return
	case held:
		r.heldSeat(m, run)
		return
	}
	_, err = r.d.LXC.Guest(ctx, r.attach(m), guest.OpRemove, []string{"--team", m.team.ID.String()}, nil)
	if ge, ok := errors.AsType[*lxccmd.GuestError](err); ok && ge.Exit == 75 {
		r.heldSeat(m, "")
		return
	}
	if err != nil {
		r.leaveFailed(ctx, g, m, "guest remove", err)
		return
	}
	m.done, m.status = true, statusDrop
	how := lineLeft
	if parked {
		how = lineRemoved
	}
	r.line(m, Changed, how)
}

// leaveFailed is a guest step's failure in the absent phase: a container that stopped leaves the
// member pending, anything else fails it with its entry left.
func (r *run) leaveFailed(ctx context.Context, g *guestBox, m *member, step string, err error) {
	if !refused(err) && r.gone(ctx, g.name, err) {
		g.gone = true
		r.leftOffline(m, whyStopped, false)
		return
	}
	m.status, m.why = lxcstate.StatusLeft, whyPending
	r.fail(m, &host.Error{Verb: verb, Step: step, Drift: refused(err), Err: fmt.Errorf("%s@%s: %w", m.decl.User, m.box, err)})
}

// heldSeat is a seat still held after the wait: deferred, the entry left, retried next apply.
// The run id is shown only when the lock record passes its grammar (TB2.1).
func (r *run) heldSeat(m *member, run string) {
	why := whyHeld
	if run != "" {
		why = whyHeldBy + run + whyRevoked
	}
	m.done, m.status, m.why = true, lxcstate.StatusLeft, why
	r.line(m, Deferred, why)
}

// waitFree is step 4, the L3 probe every SeatFreeGap for at most SeatFreeWait: held with the
// lock's run id, or stopped when the container stops during the wait, which ends it at once.
func (r *run) waitFree(ctx context.Context, g *guestBox, m *member) (bool, string, bool) {
	layout, err := paths.HomeLayout(m.acct.Home, paths.GuestRuntimeDir(m.decl.User))
	if err != nil {
		return false, "", false
	}
	lock := layout.Seat(m.team.ID).Lock()
	deadline := r.d.Clock.Now().Add(SeatFreeWait)
	for {
		held, record, err := g.root.ProbeLock(lock)
		switch {
		case err != nil && !refused(err) && !errors.Is(err, fs.ErrNotExist):
			return false, "", true
		case err != nil || !held:
			return false, "", false
		case !r.d.Clock.Now().Before(deadline):
			return true, runOf(record), false
		}
		if err := r.d.Clock.Sleep(ctx, SeatFreeGap); err != nil {
			return true, runOf(record), false
		}
		if st, err := r.d.LXC.State(ctx, g.name.String()); err != nil || st != lxccmd.Running {
			return false, "", true
		}
	}
}

// runOf is the lock record's run id when it decodes, else "".
func runOf(record []byte) string {
	rec, err := memberstate.DecodeLockRecord(record)
	if err != nil {
		return ""
	}
	return rec.Run.String()
}
