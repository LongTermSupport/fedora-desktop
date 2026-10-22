package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
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

// The absent phase's whys and lines.
const (
	whyPending   = "guest files pending"
	whyHeldBy    = "seat still held by run "
	whyHeld      = "seat still held; token revoked"
	whyRevoked   = "; token revoked"
	lineRemoved  = "parked and removed"
	lineLeft     = "removed"
	lineNoHandle = "entry removed; it never joined"
	lineNoUser   = "; its user is not in the guest"
	lineMovedTo  = "; the seat moves to "
)

// only keeps the entries the declaration gives state.
func (r *run) only(state declare.State) {
	r.members = slices.DeleteFunc(r.members, func(m *member) bool { return m.decl.State != state })
}

// absent is C 3.9.2's absent phase (LX8): each seat whose entry says absent, that the record
// names and no entry declares, or whose record entry names another user (a user move), leaves
// under its team's lock and under the record's user: park first, then the desired file without
// the seat, the seat's unit removed, the L3 wait, and `guest remove`. A container that is not
// running is parked from the host and never waited for.
func (r *run) absent(ctx context.Context) {
	r.only(declare.StateAbsent)
	// An absent entry the record does not hold has already gone: it takes no step and prints
	// nothing, so the phase converges.
	r.members = slices.DeleteFunc(r.members, func(m *member) bool { return !m.recorded })
	for _, m := range r.members {
		m.decl.User = m.prior.User
	}
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

// undeclared adds each record entry of an acted team that no entry declares, and each whose
// declared present entry names another user, as a leaver under the record's user.
func (r *run) undeclared() {
	for _, t := range r.teams {
		rec := r.records[t.ID]
		for _, e := range rec.Members {
			id := rec.SeatOf(e)
			tg := Target{Container: e.Container, Key: id.Key()}
			if r.req.Members != nil && !slices.Contains(r.req.Members, tg) {
				continue
			}
			decl, declared := r.declared(t.ID, e)
			if declared && (decl.State != declare.StatePresent || decl.User == e.User) {
				continue
			}
			m := &member{
				decl: declare.LXCMember{
					Team: t.ID, Container: e.Container.String(), User: e.User, Checkout: e.Checkout, Seat: e.Seat,
					State: declare.StateAbsent,
				},
				team: t, box: e.Container, id: id, key: tg.Key, prior: e, recorded: true, handle: e.Handle,
				target: r.req.Members != nil,
			}
			if declared {
				m.moveTo = decl.User
			}
			r.members = append(r.members, m)
		}
	}
}

// declared is the team's entry of the record entry e's seat, in any state.
func (r *run) declared(team paths.TeamID, e lxcstate.Entry) (declare.LXCMember, bool) {
	for _, m := range r.req.Decl.LXC {
		if m.Team == team && m.Container == e.Container.String() && m.Checkout == e.Checkout && m.Seat == e.Seat {
			return m, true
		}
	}
	return declare.LXCMember{}, false
}

// leaveTeam is the team's leavers under its lock: every park first, recorded left before any
// guest step, then each seat's remaining steps.
func (r *run) leaveTeam(ctx context.Context, team paths.TeamID, leavers []*member) error {
	lock, err := teamlock.Take(ctx, r.d.LockDir, team, lockVerb, host.PlayWait, r.d.Clock)
	if err != nil {
		return &host.Error{Verb: verb, Step: host.StepLock, Err: err}
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			r.errs = append(r.errs, releaseErr)
		}
	}()
	parked := r.parks(ctx, team, leavers)
	for _, m := range leavers {
		if !m.done {
			r.leave(ctx, m, slices.Contains(parked, m))
		}
	}
	return nil
}

// parks is step 1 for the team's leavers: each handle the roster has active parked (the child
// parks no other; parked, lost and retired are parked already). A left entry is never parked
// again: its handle may have joined elsewhere since. Each park is recorded left at once, so a
// crash after it cannot leave the entry over a parked handle. A seat whose park, or its record,
// fails is failed; the other leavers go on.
func (r *run) parks(ctx context.Context, team paths.TeamID, leavers []*member) []*member {
	var parkable []*member
	for _, m := range leavers {
		if m.handle != "" && m.prior.Status != lxcstate.StatusLeft {
			parkable = append(parkable, m)
		}
	}
	if len(parkable) == 0 {
		return nil
	}
	roster, err := r.d.Roster.Members(ctx, team)
	if err != nil {
		r.failAll(parkable, "roster", err)
		return nil
	}
	var parked []*member
	for _, m := range parkable {
		if roster[m.handle] != apply.MemberActive {
			continue
		}
		if err := r.d.Roster.Park(ctx, team, m.handle); err != nil {
			r.fail(m, &host.Error{Verb: verb, Step: "park", Err: err})
			continue
		}
		parked = append(parked, m)
		r.changed[team] = true
	}
	if len(parked) == 0 {
		return nil
	}
	rec, now := r.records[team], r.d.Clock.Now().UnixMilli()
	for _, m := range parked {
		e := m.prior
		e.Status, e.Why = lxcstate.StatusLeft, whyPending
		rec = rec.Set(e, now)
	}
	if _, err := lxcstate.WriteRecord(r.d.Root, rec, r.d.OwnerGID); err != nil {
		r.failAll(parked, stepRecord, err)
		return nil
	}
	r.records[team] = rec
	return parked
}

// leave is one seat's steps 2-5, after its park.
func (r *run) leave(ctx context.Context, m *member, parked bool) {
	if m.handle == "" {
		m.done, m.status = true, statusDrop
		r.line(m, Changed, lineNoHandle)
		return
	}
	if err := r.dropSeat(m); err != nil {
		r.fail(m, err)
		return
	}
	g, why, err := r.leaveBox(ctx, m.box)
	if err != nil {
		r.fail(m, &host.Error{Verb: verb, Step: stepState, Err: fmt.Errorf("container %s: %w", m.box, err)})
		return
	}
	if why != "" {
		r.leftOffline(m, why, parked)
		return
	}
	r.leaveGuest(ctx, g, m, parked)
}

// dropSeat is step 2's host half: the seat's host copy with no seat and its farewell, written
// first.
func (r *run) dropSeat(m *member) error {
	h, ok := proto.ParseHandle(m.handle)
	if !ok {
		return &host.Error{Verb: verb, Step: stepMembership, Err: fmt.Errorf("the record's handle for %s is not a handle", m.name())}
	}
	cp, _, err := lxcstate.ReadCopy(r.d.Root, m.box, m.id)
	if err != nil {
		return &host.Error{Verb: verb, Step: stepMembership, Err: err}
	}
	next, changed := cp.Next(nil, []memberstate.Farewell{{Team: m.team.ID, Seat: h.Seat, Reason: memberstate.FarewellParked}})
	if changed {
		if err := lxcstate.WriteCopy(r.d.Root, m.box, next); err != nil {
			return &host.Error{Verb: verb, Step: stepMembership, Err: err}
		}
	}
	return nil
}

// leaveBox is the seat's container, pinned once per phase, or why it is offline. lxc-ls or
// lxc-info failing is the host's fault (the error), never an offline container; a pin that
// fails after lxc-info answered running is taken as the container stopping.
func (r *run) leaveBox(ctx context.Context, c paths.ContainerName) (*guestBox, string, error) {
	for _, g := range r.boxes {
		if g.name == c {
			return g, g.offlineWhy(), g.fault
		}
	}
	g := &guestBox{name: c}
	r.boxes = append(r.boxes, g)
	names, err := r.d.LXC.Containers(ctx)
	why := ""
	if err == nil {
		why, err = boxState(ctx, r.d.LXC, names, c)
	}
	switch {
	case err != nil:
		g.fault = err
	case why == whyMissing:
		g.gone, g.missing = true, true
	case why != "":
		g.gone = true
	default:
		if g.root, err = r.d.Pin(ctx, c.String()); err != nil {
			g.gone = true
		}
	}
	return g, g.offlineWhy(), g.fault
}

// offlineWhy is a gone box's offline cause, "" for one running.
func (g *guestBox) offlineWhy() string {
	if !g.gone {
		return ""
	}
	return g.why()
}

// why is a gone box's offline cause.
func (g *guestBox) why() string {
	if g.missing {
		return whyMissing
	}
	return whyStopped
}

// leftOffline is a leaving seat whose container is not running: parked from the host, its entry
// kept left with the guest files pending; a missing container's has no guest files left, so its
// host copy and entry are dropped, a user move's entry rewritten instead (n3-D12).
func (r *run) leftOffline(m *member, why string, parked bool) {
	m.done, m.status, m.why = true, lxcstate.StatusLeft, why+"; "+whyPending
	switch {
	case why == whyMissing:
		if err := lxcstate.DeleteCopy(r.d.Root, m.box, m.key); err != nil {
			r.errs = append(r.errs, &host.Error{Verb: verb, Step: stepMembership, Err: err})
			return
		}
		m.status = r.removedStatus(m)
		r.line(m, Changed, "parked; entry dropped ("+why+")"+r.movedTo(m))
	case parked:
		r.line(m, Changed, "parked; "+whyPending+" ("+why+")")
	default:
		r.line(m, Skipped, m.why)
	}
	if m.target {
		r.errs = append(r.errs, &OfflineError{Container: m.box, Why: why})
	}
}

// leaveGuest is steps 2-5 in a running guest. A leaver whose user the guest's passwd no longer
// names has no file the host can reach as that user: only its unit is removed.
func (r *run) leaveGuest(ctx context.Context, g *guestBox, m *member, parked bool) {
	passwd, err := readGuest(g.root, guestPasswd)
	if err == nil {
		m.acct, err = guestfacts.Passwd(passwd, m.decl.User, r.d.HostUIDs)
	}
	if errors.Is(err, guestfacts.ErrNoAccount) {
		if err = r.removeUnit(ctx, g, m); err != nil && !unitDown(err) {
			r.leaveFailed(ctx, g, m, "unit", err)
			return
		}
		r.removed(m, parked, lineNoUser)
		return
	}
	if err != nil {
		r.leaveFailed(ctx, g, m, stepReads, err)
		return
	}
	cp, _, err := lxcstate.ReadCopy(r.d.Root, m.box, m.id)
	if err == nil {
		err = r.desired(ctx, m, cp)
	}
	if err != nil {
		r.leaveFailed(ctx, g, m, stepMembership, err)
		return
	}
	if err = r.removeUnit(ctx, g, m); err != nil && !unitDown(err) {
		r.leaveFailed(ctx, g, m, "unit", err)
		return
	}
	r.removeGuest(ctx, g, m, parked)
}

// removeGuest is steps 4 and 5: the L3 wait, then `guest remove`.
func (r *run) removeGuest(ctx context.Context, g *guestBox, m *member, parked bool) {
	wait, err := r.waitFree(ctx, g, m)
	switch {
	case err != nil:
		m.status, m.why = lxcstate.StatusLeft, whyPending
		r.fail(m, &host.Error{Verb: verb, Step: "wait", Err: fmt.Errorf("%s: %w", m.name(), err)})
		return
	case wait.stopped:
		r.leftOffline(m, whyStopped, false)
		return
	case wait.held:
		r.heldSeat(m, wait.run)
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
	r.removed(m, parked, "")
}

// removed is a seat whose guest files are gone: its host copy deleted, then its entry dropped,
// or a user move's rewritten for the new user.
func (r *run) removed(m *member, parked bool, note string) {
	if err := lxcstate.DeleteCopy(r.d.Root, m.box, m.key); err != nil {
		m.status, m.why = lxcstate.StatusLeft, whyPending
		r.fail(m, &host.Error{Verb: verb, Step: stepMembership, Err: err})
		return
	}
	m.done, m.status = true, r.removedStatus(m)
	how := lineLeft
	if parked {
		how = lineRemoved
	}
	r.line(m, Changed, how+note+r.movedTo(m))
}

// removedStatus is a removed seat's entry: dropped, or rewritten for a user move.
func (r *run) removedStatus(m *member) string {
	if m.moveTo.String() != "" {
		return statusMove
	}
	return statusDrop
}

func (r *run) movedTo(m *member) string {
	if m.moveTo.String() == "" {
		return ""
	}
	return lineMovedTo + m.moveTo.String()
}

// leaveFailed is a guest step's failure in the absent phase: a container that stopped leaves the
// seat pending, anything else fails it with its entry left.
func (r *run) leaveFailed(ctx context.Context, g *guestBox, m *member, step string, err error) {
	if !refused(err) && r.gone(ctx, g.name, err) {
		g.gone = true
		r.leftOffline(m, whyStopped, false)
		return
	}
	m.status, m.why = lxcstate.StatusLeft, whyPending
	r.fail(m, &host.Error{Verb: verb, Step: step, Drift: refused(err), Err: fmt.Errorf("%s: %w", m.name(), err)})
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

// waitFree is step 4, the L3 probe of the leaving seat's own key every SeatFreeGap for at most
// SeatFreeWait: held with the lock's run id, or stopped when the container stops during the
// wait, which ends it at once. A probe that fails is stopped only when lxc-info then answers
// that the container is not running; otherwise it, a layout that cannot be made, or lxc-info
// failing is the error, as is a wait cut short by its context.
func (r *run) waitFree(ctx context.Context, g *guestBox, m *member) (seatWait, error) {
	layout, err := paths.LXCLayout(m.acct.Home, paths.GuestRuntimeDir(m.key), m.key)
	if err != nil {
		return seatWait{}, err
	}
	lock := layout.Seat(m.team.ID).Lock()
	deadline := r.d.Clock.Now().Add(SeatFreeWait)
	for {
		held, record, err := g.root.ProbeLock(lock)
		if refused(err) || errors.Is(err, fs.ErrNotExist) {
			return seatWait{}, nil
		}
		if err != nil {
			return r.probeFailed(ctx, g, err)
		}
		switch {
		case !held:
			return seatWait{}, nil
		case !r.d.Clock.Now().Before(deadline):
			return seatWait{held: true, run: runOf(record)}, nil
		}
		if err := r.d.Clock.Sleep(ctx, SeatFreeGap); err != nil {
			return seatWait{}, err
		}
		if stopped, err := r.stopped(ctx, g); err != nil || stopped {
			return seatWait{stopped: stopped}, err
		}
	}
}

// seatWait is the L3 wait's answer: the seat still held (with the lock's run id, when it passes
// its grammar), or its container stopped; neither is a free seat.
type seatWait struct {
	held    bool
	run     string
	stopped bool
}

// probeFailed is a lock probe's error: the container stopped when lxc-info answers so, else the
// error (lxc-info's own first).
func (r *run) probeFailed(ctx context.Context, g *guestBox, probeErr error) (seatWait, error) {
	stopped, err := r.stopped(ctx, g)
	switch {
	case err != nil:
		return seatWait{}, err
	case stopped:
		return seatWait{stopped: true}, nil
	}
	return seatWait{}, probeErr
}

// stopped is lxc-info's answer that g is not running.
func (r *run) stopped(ctx context.Context, g *guestBox) (bool, error) {
	st, err := r.d.LXC.State(ctx, g.name.String())
	return err == nil && st != lxccmd.Running, err
}

// runOf is the lock record's run id when it decodes, else "".
func runOf(record []byte) string {
	rec, err := memberstate.DecodeLockRecord(record)
	if err != nil {
		return ""
	}
	return rec.Run.String()
}
