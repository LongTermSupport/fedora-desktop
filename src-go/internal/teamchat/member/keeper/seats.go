package keeper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// seat is one seat the session holds: its lock, the link's last report of it, and a removal
// under way or a leave notice still owed.
type seat struct {
	team      paths.TeamID
	name      string
	path      paths.Seat
	lock      *lock.SeatLock
	startedMS int64
	st        *wire.State
	removing  *removal
	owedTxn   string
	owedTimer *timer
}

// removal is a remove sent to the link and not yet answered (LN8).
type removal struct {
	txn      string
	farewell *string
	timer    *timer
}

func (k *keeper) seatOf(team paths.TeamID) *seat {
	for _, s := range k.seats {
		if s.team == team {
			return s
		}
	}
	return nil
}

// cancelSeat disarms s's waits.
func (k *keeper) cancelSeat(s *seat) {
	if s.removing != nil {
		k.cancel(s.removing.timer)
	}
	k.cancel(s.owedTimer)
}

// writeKeeper writes keeper.json, the status line's view of the link.
func (k *keeper) writeKeeper() {
	var session *paths.SessionName
	if kind := k.cfg.Launch.Kind; k.desired != nil && (kind == cli.KindCCY || kind == cli.KindLXC) {
		name := k.desired.Session
		session = &name
	}
	rec := state.Keeper{
		V: state.Version, Run: k.runID, Link: k.linkStatus, Why: k.linkWhy(), Restarts: k.restarts,
		NextMS: k.nextMS, Kind: string(k.cfg.Launch.Kind), Session: session, Desired: k.desiredState,
	}
	if err := state.Write(k.runRoot, k.run.KeeperJSON(), rec); err != nil {
		k.logf("write keeper.json: %v", err)
	}
}

// linkWhy is why the link failed, nil unless it has.
func (k *keeper) linkWhy() *string {
	if k.linkStatus != state.LinkFailed {
		return nil
	}
	return k.why
}

// writeReports writes the report of every held seat whose handle the link has named.
func (k *keeper) writeReports() {
	for _, s := range k.seats {
		k.writeReport(s)
	}
}

func (k *keeper) writeReport(s *seat) {
	if s.st == nil || s.lock == nil {
		return
	}
	why := k.linkWhy()
	if k.failedCmd {
		failed := state.WhyLaunchFailed
		why = &failed
	}
	r := state.Report{
		V: state.Version, Run: k.runID, StartedMS: s.startedMS,
		Seat: state.SeatRef{Team: s.team, Seat: s.name}, Handle: s.st.Handle, Link: k.linkStatus,
		Why: why, LastSyncOKMS: s.st.LastSyncOKMS, Pending: s.st.Pending, Delivery: s.st.Delivery,
		Desired: k.reportDesired(), Store: s.st.Store, NeedsRelaunch: k.needsRelaunch(),
	}
	if err := state.Write(k.stateRoot, s.path.Report(), r); err != nil {
		k.logf("seat %s: write report.json: %v", s.team, err)
	}
}

func (k *keeper) reportDesired() state.ReportDesired {
	var gen int64
	if k.desired != nil {
		gen = k.desired.Generation
	}
	return state.ReportDesired{Generation: gen, State: k.desiredState, Code: k.desiredCode}
}

// needsRelaunch is a Claude Code session launched with inbound refuse whose desired membership
// now accepts: only a relaunch takes the new settings.
func (k *keeper) needsRelaunch() bool {
	return k.cfg.Launch.Mode == cli.LaunchClaude && k.inbound == state.InboundRefuse &&
		k.desired != nil && k.desired.Inbound == state.InboundAccept
}

// onState is the link's report of one team, the seat's report.json written from it.
func (k *keeper) onState(st wire.State) {
	s := k.seatOf(st.Team)
	if s == nil {
		k.logf("the link reported team %s, which the session does not hold", st.Team)
		return
	}
	s.st = &st
	k.writeReport(s)
}

// onWatch is RC1: an event naming desired.json, or an overflow, re-reads it; a watch that ends,
// or whose directory goes, is watched again.
func (k *keeper) onWatch(ev WatchEvent, ok bool) {
	switch {
	case !ok:
		k.watchLost("the watch ended")
	case ev.Op&(unix.IN_IGNORED|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0:
		k.watchLost("its directory went")
	case ev.Name == filepath.Base(k.cfg.Layout.DesiredFile()) || ev.Op&unix.IN_Q_OVERFLOW != 0:
		k.reread()
	}
}

// watchLost stops following the old watch and watches the desired directory again, unless a
// retry is already due.
func (k *keeper) watchLost(why string) {
	k.watch = nil
	k.logf("the desired membership is no longer watched (%s): watching it again", why)
	if k.rewatchTimer == nil {
		k.rewatch()
	}
}

// rewatch watches the desired directory and re-reads the file; while the directory cannot be
// watched the membership is refused missing, and the watch is tried again every rewatchGap.
func (k *keeper) rewatch() {
	k.rewatchTimer = nil
	if k.phase == ending {
		return
	}
	dir := k.cfg.Layout.Desired()
	ch, err := k.cfg.Watcher.Watch(dir)
	if err != nil {
		k.logf("watch %s: %v", dir, err)
		code := state.CodeMissing
		k.desiredState, k.desiredCode = state.DesiredRefused, &code
		k.publishDesired()
		k.rewatchTimer = k.after(rewatchGap, k.rewatch)
		return
	}
	k.watch = ch
	k.reread()
}

// reread applies the desired membership afresh; a refused read keeps the last good one and
// says why (B 2.10).
func (k *keeper) reread() {
	if k.linkStatus == state.LinkFailed || k.phase == ending {
		return
	}
	d, err := k.readDesired()
	if err != nil {
		code := state.CodeMissing
		if inv, isInvalid := errors.AsType[*state.InvalidError](err); isInvalid {
			code = inv.Code
		}
		k.logf("the desired membership is refused (%s): %v", code, err)
		k.desiredState, k.desiredCode = state.DesiredRefused, &code
		k.publishDesired()
		return
	}
	k.desired, k.desiredState, k.desiredCode = &d, state.DesiredApplied, nil
	k.logf("the desired membership generation %d applied", d.Generation)
	k.publishDesired()
	k.reconcile()
}

func (k *keeper) publishDesired() {
	k.writeKeeper()
	k.writeReports()
	if k.link != nil {
		rd := k.reportDesired()
		k.tell(k.link, wire.Msg{Kind: wire.KindDesired, Desired: &rd}, nil)
	}
}

// reconcile makes the held seats the desired ones: a seat no longer wanted, or wanted under
// another seat name, is removed, and a wanted team with none is added.
func (k *keeper) reconcile() {
	if k.phase != running || k.linkStatus == state.LinkFailed || k.desired == nil {
		return
	}
	wanted := map[paths.TeamID]string{}
	for _, d := range k.desired.Seats {
		wanted[d.Team] = d.Seat
	}
	for _, s := range slices.Clone(k.seats) {
		if s.removing != nil || s.owedTxn != "" {
			continue
		}
		if name, ok := wanted[s.team]; !ok || name != s.name {
			k.remove(s)
		}
	}
	for _, d := range k.desired.Seats {
		if k.seatOf(d.Team) == nil {
			k.add(want{d.Team, d.Seat})
		}
	}
}

// add takes a seat while the session runs; one another session holds is refused to the link,
// with its holder's run.
func (k *keeper) add(w want) {
	// A hot add's lock tries are bounded by Locks; nothing cancels them mid-loop.
	s, err := k.takeSeat(context.Background(), w)
	if errors.Is(err, lock.ErrBusy) {
		k.refuse(w.team)
		return
	}
	if err != nil {
		return
	}
	k.seats = append(k.seats, s)
	if k.link == nil {
		return
	}
	fd, err := s.lock.Dup()
	if err != nil {
		k.logf("seat %s: dup its lock: %v", s.team, err)
		return
	}
	k.sendAdd(k.link, s, fd)
}

func (k *keeper) refuse(team paths.TeamID) {
	f, err := k.stateRoot.Read(k.cfg.Layout.Seat(team).Lock(), state.MaxLockRecord)
	var rec state.LockRecord
	if err == nil {
		rec, err = state.DecodeLockRecord(f.Data)
	}
	if err != nil {
		k.logf("seat %s is held by another session, whose run cannot be read: %v", team, err)
		return
	}
	k.logf("seat %s is held by run %s", team, rec.Run)
	if k.link != nil {
		k.tell(k.link, wire.Msg{Kind: wire.KindRefused, Refused: &wire.Refused{Team: team, HolderRun: rec.Run}}, nil)
	}
}

// remove takes a seat out (LN8): the link is told and has 10 s to answer; with no link up the
// keeper deletes the store itself.
func (k *keeper) remove(s *seat) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		k.logf("seat %s: draw a transaction id: %v", s.team, err)
		return
	}
	s.removing = &removal{txn: "tc" + hex.EncodeToString(b[:]), farewell: k.farewell(s)}
	k.logf("seat %s: removing (%s)", s.team, s.removing.txn)
	if k.link == nil {
		k.deleteStore(s)
		k.removed(s, "")
		return
	}
	rm := wire.Remove{Team: s.team, Farewell: s.removing.farewell, Txn: s.removing.txn}
	k.tell(k.link, wire.Msg{Kind: wire.KindRemove, Remove: &rm}, nil)
	s.removing.timer = k.after(removeWait, func() {
		k.logf("seat %s: the link did not answer the remove", s.team)
		k.killLink()
	})
}

// farewell is why the desired membership took s out, nil for no reason.
func (k *keeper) farewell(s *seat) *string {
	for _, f := range k.desired.Farewell {
		if f.Team == s.team && f.Seat == s.name {
			reason := f.Reason
			return &reason
		}
	}
	return nil
}

// settleRemovals is a link's exit with removals unanswered: the keeper deletes each store
// under its lock and treats the seat as removed, its user id unknown.
func (k *keeper) settleRemovals() {
	for _, s := range slices.Clone(k.seats) {
		if s.removing == nil {
			continue
		}
		k.cancel(s.removing.timer)
		failpoint.Hit(failpoint.KeeperBeforeStoreDelete)
		k.deleteStore(s)
		k.removed(s, "")
	}
}

func (k *keeper) deleteStore(s *seat) {
	w, err := state.NewStoreWriter(k.stateRoot, s.lock)
	if err != nil {
		k.logf("seat %s: open its store: %v", s.team, err)
		return
	}
	if err := w.Remove(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		k.logf("seat %s: delete its store: %v", s.team, err)
	}
	k.closeQuietly(w)
}

func (k *keeper) onRemoved(r wire.Removed) {
	s := k.seatOf(r.Team)
	if s == nil || s.removing == nil {
		k.logf("the link removed team %s, which the keeper is not removing", r.Team)
		return
	}
	k.cancel(s.removing.timer)
	// The link may not have deleted the store (it did not hold the team, or its delete failed),
	// so the keeper deletes it under the lock every time; a store already gone is no error.
	k.deleteStore(s)
	k.removed(s, r.UserID)
	k.reconcile()
}

// removed ends a removal: a released seat owes its leave notice, so its lock is kept until
// the link has posted it or 30 s have passed; any other closes the lock at once.
func (k *keeper) removed(s *seat, userID string) {
	if userID != "" {
		k.removedIDs[s.team] = append(k.removedIDs[s.team], userID)
	}
	rm := s.removing
	s.removing = nil
	if rm.farewell != nil && *rm.farewell == state.FarewellReleased {
		s.owedTxn = rm.txn
		k.logf("seat %s: its leave notice is owed", s.team)
		s.owedTimer = k.after(owedWait, func() {
			k.logf("seat %s: the leave notice was not posted in time", s.team)
			k.drop(s)
			k.reconcile()
		})
		return
	}
	k.drop(s)
}

func (k *keeper) onLeft(team paths.TeamID) {
	s := k.seatOf(team)
	if s == nil || s.owedTxn == "" {
		k.logf("the link left team %s, which owes no notice", team)
		return
	}
	k.drop(s)
	k.reconcile()
}

// drop lets a seat go: its lock closed, and the session no longer holds it.
func (k *keeper) drop(s *seat) {
	k.cancelSeat(s)
	k.closeSeatLock(s)
	k.seats = slices.DeleteFunc(k.seats, func(x *seat) bool { return x == s })
	k.logf("seat %s released", s.team)
}
