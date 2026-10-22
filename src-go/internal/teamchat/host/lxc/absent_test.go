package lxc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
	u := agent1(t)
	l, err := paths.HomeLayout("/srv/agent1", paths.GuestRuntimeDir(u))
	if err != nil {
		t.Fatal(err)
	}
	return l.Seat(mustTeam(t, team)).Lock()
}

// holdSeat plants the seat lock with record and holds it LOCK_EX until the test ends.
func holdSeat(t *testing.T, b *box, record string) func() {
	t.Helper()
	p := filepath.Join(b.dir, seatLock(t, "t1"))
	writeTree(t, b.dir, map[string]string{seatLock(t, "t1")[1:]: record})
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
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
	want := []string{"park " + firstHandle, "guest membership", "systemd Stop agent1", "systemd Reload", "guest remove"}
	if !slices.Equal(w.log, want) {
		t.Fatalf("order %v, want %v", w.log, want)
	}
	if !strings.Contains(w.lxc.calls[0].Stdin, `"farewell":[{"team":"t1","seat":"1","reason":"parked"}]`) || !strings.Contains(w.lxc.calls[0].Stdin, `"seats":[]`) {
		t.Fatalf("desired %s", w.lxc.calls[0].Stdin)
	}
	hasLine(t, r, "CHANGED\tt1\tagent1@c1\tparked and removed")
	if _, found := w.record().Find(mustBox(t, "c1"), agent1(t)); found {
		t.Fatal("a removed member kept its entry")
	}
	if w.clock.sleeps != 0 {
		t.Fatalf("slept %d times for a free seat", w.clock.sleeps)
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
	hasLine(t, r, "DEFERRED\tt1\tagent1@c1\tseat still held by run "+runID+"; token revoked")
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
	if _, found := w.record().Find(mustBox(t, "c1"), agent1(t)); found {
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
	hasLine(t, r, "DEFERRED\tt1\tagent1@c1\tseat still held; token revoked")
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
	hasLine(t, r, "SKIPPED\tt1\tagent1@c1\toffline: container stopped; guest files pending")
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
	hasLine(t, r, "CHANGED\tt1\tagent1@c1\tparked; guest files pending (offline: container stopped)")
	if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
		t.Fatalf("entry %+v", e)
	}
	if w.clock.sleeps != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("waited or attached for a stopped container: %d %v", w.clock.sleeps, w.lxc.verbs())
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), agent1(t))
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
	hasLine(t, r, "CHANGED\tt1\tagent1@c1\tparked; entry dropped (offline: container missing)")
	if _, found := w.record().Find(mustBox(t, "c1"), agent1(t)); found {
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
	hasLine(t, r, "DEFERRED\tt1\tagent1@c1\tseat still held; token revoked")
}

func TestAbsentKeepsTheUnitForAnotherTeam(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1", "t2"}, w.member("t1", "c1"), w.member("t2", "c1"))
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	w.bus.calls = nil
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1", "t2"}, m, w.member("t2", "c1"))
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if len(w.bus.calls) != 0 {
		t.Fatalf("the unit of a user still in t2 was touched: %v", w.bus.calls)
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), agent1(t))
	if err != nil || len(cp.Seats) != 1 || cp.Seats[0].Team.String() != "t2" {
		t.Fatalf("copy %+v %v", cp, err)
	}
}
