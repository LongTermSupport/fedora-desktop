package lxc

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

// leaving is a placed world whose entry now says absent.
func leaving(t *testing.T) *world {
	t.Helper()
	w := placed(t)
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1"}, m)
	return w
}

// seatLock is the guest path of agent1's seat lock for team.
func seatLock(t *testing.T, team string) string {
	t.Helper()
	k := keyOf(t, team)
	l, err := paths.LXCLayout("/srv/agent1", paths.GuestRuntimeDir(k), k)
	if err != nil {
		t.Fatal(err)
	}
	return l.Seat(mustTeam(t, team)).Lock()
}

// holdSeat plants the seat lock with record and holds it LOCK_EX until the test ends.
func holdSeat(t *testing.T, b *box, record string) func() {
	t.Helper()
	lock := seatLock(t, "t1")[1:]
	writeTree(t, b.dir, map[string]string{lock: record})
	f := testfiles.Open(t, b.dir, lock)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

func TestAbsentParksThenRemovesInOrder(t *testing.T) {
	w := leaving(t)
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"park " + firstHandle, "guest membership", "systemd Stop " + unitOf(t, "t1"), "systemd Reload", "guest remove"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("order %v, want %v", w.log, want)
	}
	if !strings.Contains(w.lxc.calls[0].Stdin, `"farewell":[{"team":"t1","seat":"1","reason":"parked"}]`) || !strings.Contains(w.lxc.calls[0].Stdin, `"seats":[]`) {
		t.Fatalf("desired %s", w.lxc.calls[0].Stdin)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked and removed"))
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("a removed member kept its entry")
	}
	if w.clock.sleeps != 0 {
		t.Fatalf("slept %d times for a free seat", w.clock.sleeps)
	}
}

// removing is a placed world whose team t1 is now declared absent, with no LXC entry (an entry
// of an absent team is refused by declare.Decode and by the play).
func removing(t *testing.T) *world {
	t.Helper()
	w := placed(t)
	w.declare(nil)
	gone := w.team("t1")
	gone.State = declare.StateAbsent
	w.decl.Teams = []declare.Team{gone}
	return w
}

// D 5.7 step 5 removes a team and its LXC entries in one run, so the absent phase acts on a team
// declared absent: every seat its record holds is parked and removed while its homeserver still
// runs, before `team remove`, and the record is left empty. --only-teams may name it.
func TestAbsentTeamsSeatsLeaveBeforeItsRemoval(t *testing.T) {
	w := removing(t)
	r, err := w.apply(PhaseAbsent, "t1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"park " + firstHandle, "guest membership", "systemd Stop " + unitOf(t, "t1"), "systemd Reload", "guest remove"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("order %v, want %v", w.log, want)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked and removed"))
	if rec := w.record(); len(rec.Members) != 0 {
		t.Fatalf("the absent team's record still holds %+v", rec.Members)
	}
	if _, err := w.apply(PhasePresent, "t1"); err == nil || !strings.Contains(err.Error(), "not a present team") {
		t.Fatalf("the present phase scoped to an absent team: %v", err)
	}
}

// A seat of an absent team in a stopped container is parked from the host and kept left; the
// next run, with the container running, removes its guest files without parking it again.
func TestAbsentTeamsStoppedSeatIsParkedThenCleaned(t *testing.T) {
	w := removing(t)
	b := w.lxc.boxes["c1"]
	b.state = lxccmd.Stopped
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
		t.Fatalf("entry %+v", e)
	}
	b.state = lxccmd.Running
	w.roster.parks, w.log = nil, nil
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.roster.parks) != 0 || !slices.Contains(w.log, "guest remove") {
		t.Fatalf("parks %v, log %v", w.roster.parks, w.log)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "removed"))
	if rec := w.record(); len(rec.Members) != 0 {
		t.Fatalf("record %+v", rec.Members)
	}
}

// An absent team that is no longer installed has no roster to park on: a seat its record holds
// that is not left is refused by name before any change, never passed by; one only left (parked
// already) is cleaned from the guest as for an installed team.
func TestAbsentTeamNotInstalled(t *testing.T) {
	w := removing(t)
	w.unstamped = map[string]bool{"t1": true}
	_, err := w.apply(PhaseAbsent)
	if err == nil || !strings.Contains(err.Error(), "team t1 is declared absent and not installed, but its LXC record holds seats that were never parked") {
		t.Fatalf("err %v", err)
	}
	if len(w.log) != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("log %v, calls %v", w.log, w.lxc.calls)
	}
	e := w.entry("c1")
	e.Status, e.Why = lxcstate.StatusLeft, whyPending
	if _, err := lxcstate.WriteRecord(w.host, w.record().Set(e, 1), w.deps().OwnerGID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if len(w.roster.parks) != 0 || !slices.Contains(w.log, "guest remove") || len(w.record().Members) != 0 {
		t.Fatalf("parks %v, log %v, record %+v", w.roster.parks, w.log, w.record().Members)
	}
}

// The play runs the absent phase before `team apply`, so on a team's first play the team is
// declared, approved and has LXC entries, but no stamp: nothing of it has joined, so nothing
// leaves, and the phase passes it by. A team redeclared after its removal has no stamp either,
// and its record may still hold seats; each that would leave is DEFERRED, visible, until the
// next run, which follows `team apply`, takes its steps. The present phase still needs the stamp.
func TestAbsentPassesByATeamNeverApplied(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.unstamped = map[string]bool{"t1": true}
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 0 || len(w.log) != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("lines %v, log %v, calls %v", r.Lines, w.log, w.lxc.calls)
	}
	if _, err = w.apply(PhaseAbsent, "t1"); err != nil {
		t.Fatalf("scoped to the unstamped team: %v", err)
	}
	if _, err = w.apply(PhasePresent); err == nil || !strings.Contains(err.Error(), "no install stamp") {
		t.Fatalf("present phase of an unstamped team: %v", err)
	}
	p := placed(t)
	p.unstamped = map[string]bool{"t1": true}
	if r, err = p.apply(PhaseAbsent); err != nil || len(r.Lines) != 0 {
		t.Fatalf("an unstamped team whose recorded seat stays declared: %v, %v", r.Lines, err)
	}
	p.declare([]string{"t1"})
	r, err = p.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", whyNotInstalled))
	if len(p.log) != 0 || len(p.lxc.calls) != 0 || p.entry("c1").Status != lxcstate.StatusJoined {
		t.Fatalf("an unstamped team's seat took a step: log %v, calls %v, entry %+v", p.log, p.lxc.calls, p.entry("c1"))
	}
}

func TestAbsentUndeclaredEntryLeaves(t *testing.T) {
	w := placed(t)
	w.declare([]string{"t1"})
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.roster.parks, "t1 "+firstHandle) {
		t.Fatalf("parks %v", w.roster.parks)
	}
}

func TestAbsentHeldSeatDeferredThenRetried(t *testing.T) {
	w := leaving(t)
	b := w.lxc.boxes["c1"]
	release := holdSeat(t, b, `{"v":1,"run":"`+runID+`","started_ms":1}`)
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "seat still held by run ")+runID+"; token revoked")
	if w.clock.sleeps != int(SeatFreeWait/SeatFreeGap) {
		t.Fatalf("slept %d times", w.clock.sleeps)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusLeft || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	if slices.Contains(w.lxc.verbs(), "c1 guest remove") {
		t.Fatal("removed a held seat")
	}
	release()
	w.roster.parks = nil
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if len(w.roster.parks) != 0 {
		t.Fatalf("a left member was parked again: %v", w.roster.parks)
	}
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("the retry did not remove the member")
	}
}

func TestAbsentHeldSeatWithAForgedRecordNamesNoRun(t *testing.T) {
	w := leaving(t)
	holdSeat(t, w.lxc.boxes["c1"], `{"v":1,"run":"not-a-run","started_ms":1}`)
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "seat still held; token revoked"))
}

func TestAbsentStopsWaitingWhenTheContainerStops(t *testing.T) {
	w := leaving(t)
	b := w.lxc.boxes["c1"]
	holdSeat(t, b, `{"v":1,"run":"`+runID+`","started_ms":1}`)
	w.clock.onSleep = func() { b.state = lxccmd.Stopped }
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if w.clock.sleeps != 1 {
		t.Fatalf("slept %d times after the container stopped", w.clock.sleeps)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: container stopped; guest files pending"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
		t.Fatalf("entry %+v", e)
	}
}

func TestAbsentOfflineContainers(t *testing.T) {
	w := leaving(t)
	b := w.lxc.boxes["c1"]
	b.state = lxccmd.Stopped
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked; guest files pending (offline: container stopped)"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
		t.Fatalf("entry %+v", e)
	}
	if w.clock.sleeps != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("waited or attached for a stopped container: %d %v", w.clock.sleeps, w.lxc.verbs())
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1"))
	if err != nil || len(cp.Seats) != 0 {
		t.Fatalf("the host copy kept the seat: %+v %v", cp, err)
	}
	b.state = lxccmd.Running
	if _, err = w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.lxc.verbs(), "c1 guest membership") || !slices.Contains(w.lxc.verbs(), "c1 guest remove") {
		t.Fatalf("the pending guest files were not cleaned: %v", w.lxc.verbs())
	}

	w = leaving(t)
	delete(w.lxc.boxes, "c1")
	r, err = w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked; entry dropped (offline: container missing)"))
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("a missing container's member kept its entry")
	}
}

func TestAbsentHeldByOp75IsDeferred(t *testing.T) {
	w := leaving(t)
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest remove" {
			return &lxccmd.GuestError{Op: c.Verb, Exit: 75, Code: "busy"}
		}
		return nil
	}
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "seat still held; token revoked"))
}

// Only an enrolled handle is parked: one the roster has parked, lost or retired already (a crash
// after the park, a return answered retired) leaves without a park, and the seat still goes.
func TestAbsentParksOnlyAnEnrolledHandle(t *testing.T) {
	for _, state := range []string{apply.MemberParked, apply.MemberLost, apply.MemberRetired, ""} {
		w := leaving(t)
		if state == "" {
			delete(w.roster.roster[mustTeam(t, "t1")], firstHandle)
		} else {
			w.roster.roster[mustTeam(t, "t1")][firstHandle] = state
		}
		r, err := w.apply(PhaseAbsent)
		if err != nil {
			t.Fatalf("%q: %v", state, err)
		}
		if len(w.roster.parks) != 0 {
			t.Fatalf("%q: parked %v", state, w.roster.parks)
		}
		hasLine(t, r, lineOf(t, "CHANGED", "c1", lineLeft))
		if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
			t.Fatalf("%q: the seat kept its entry", state)
		}
	}
}

// The park is recorded, the entry left, before any guest step, so a crash in the wait cannot
// leave a joined entry over a parked handle.
func TestAbsentRecordsTheParkBeforeTheGuest(t *testing.T) {
	w := leaving(t)
	var seen []string
	w.lxc.answer = func(c guestCall) error {
		e := w.entry("c1")
		seen = append(seen, c.Verb+" "+e.Status)
		return nil
	}
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []string{"guest membership left", "guest remove left"}) {
		t.Fatalf("the record during the guest steps: %v", seen)
	}
}

// A failing roster read fails the team's leavers with handles, and the lock is still released.
func TestAbsentRosterFailureFailsTheLeavers(t *testing.T) {
	w := leaving(t)
	w.roster.membersErr = errors.New("child down")
	if _, err := w.apply(PhaseAbsent); err == nil || !strings.Contains(err.Error(), "child down") {
		t.Fatalf("err %v", err)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("entry %+v", e)
	}
	w.roster.membersErr = nil
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatalf("the lock was kept: %v", err)
	}
}

// An absent entry the record does not hold has nothing to leave: no line, the steady state
// once a seat has gone, so the phase converges.
func TestAbsentUnrecordedEntryIsSilent(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1"}, m)
	for range 2 {
		r, err := w.apply(PhaseAbsent)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Lines) != 0 || r.Summary != "0 changed, 0 skipped, 0 deferred" {
			t.Fatalf("lines %v, summary %q", lines(r), r.Summary)
		}
	}
	if len(w.roster.parks) != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("parked %v or attached %v", w.roster.parks, w.lxc.verbs())
	}
}

func TestAbsentStopsOnlyItsOwnSeatsUnit(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1", "t2"}, w.member("t1", "c1"), w.member("t2", "c1"))
	joined, joinErr := w.apply(PhasePresent)
	if joinErr != nil {
		t.Fatal(joinErr)
	}
	hasLine(t, joined, lineAt(t, "CHANGED", "t2", "agent1", "c1", 1, "joined as "+firstHandle))
	w.bus.calls = nil
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1", "t2"}, m, w.member("t2", "c1"))
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Stop " + unitOf(t, "t1"), "Reload"}) {
		t.Fatalf("systemd %v, want only t1's unit stopped", w.bus.calls)
	}
	if _, ok := guestFile(t, b, unitFileOf(t, "t2")); !ok {
		t.Fatal("t2's unit was removed")
	}
	if _, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1")); err != nil || found {
		t.Fatalf("t1's copy kept: %v %v", found, err)
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t2"))
	if err != nil || len(cp.Seats) != 1 || cp.Seats[0].Team.String() != "t2" {
		t.Fatalf("t2's copy %+v %v", cp, err)
	}
}
