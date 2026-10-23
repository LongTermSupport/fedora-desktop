package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// timeoutArmed is whether the engine's one TIMEOUT timer is armed, read on the state goroutine.
// A test names that timer so and by dueAt, never by a count of the clock's waiters: the
// delivery loop, the proof and status.json arm their own timers on the same clock
// (project_fakeclock_count).
func (w *world) timeoutArmed() bool {
	w.t.Helper()
	var armed bool
	w.local(func() { armed = w.e.timer != nil })
	return armed
}

// dueAt is whether the clock holds a waiter due at wall time atMS, as the TIMEOUT timer is
// armed from wall milliseconds.
func (w *world) dueAt(atMS int64) bool {
	return w.clk.Armed(time.Duration(atMS-w.clk.WallMS()) * time.Millisecond)
}

// timeoutDue is when a ping's TIMEOUT falls due: past its deadline and A 8.3's grace.
func timeoutDue(p state.Ping) int64 {
	return p.DueMS + timeoutGraceMS + 1
}

// SD4-6: never a tick. Nothing owed arms nothing; three pings owed arm one timer.
func TestSD4_6OneTimerAndOnlyWhenOwed(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	answered := outPing(1, startWall, agent(ops, 2))
	answered.Answered = []int{0}
	w.heldWith(ops, w.pingStore(ops, answered))
	if w.timeoutArmed() || w.dueAt(timeoutDue(answered)) {
		t.Fatal("a TIMEOUT timer with nothing owed")
	}
	first, second, third := outPing(2, startWall, agent(dev, 2)), outPing(3, startWall+1, agent(dev, 3)), outPing(4, startWall+2, agent(dev, 2))
	w.heldWith(dev, w.pingStore(dev, first, second, third))
	w.e.SetHealth(dev, team.Health{Down: true})
	if !w.timeoutArmed() || !w.dueAt(timeoutDue(first)) || w.dueAt(timeoutDue(second)) || w.dueAt(timeoutDue(third)) {
		t.Fatal("three pings owed did not arm the one timer, for the first")
	}
	w.advanceTo(startWall + pingTimeoutMS + timeoutGraceMS + 3)
	if n := len(timeouts(w.store(dev))); n != 3 {
		t.Fatalf("TIMEOUTs = %d", n)
	}
	if w.timeoutArmed() {
		t.Fatal("a TIMEOUT timer once every TIMEOUT is made")
	}
}

// SD4-6: a wall clock stepped back makes the firing re-arm without writing.
func TestSD4_6AWallStepBackReArmsWithoutWriting(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.advanceTo(p.DueMS + timeoutGraceMS)
	before, err := w.root.Read(w.layout.Seat(ops).Store(), 1<<22)
	check(t, err)
	w.clk.StepWall(-120_000)
	w.clk.Advance(time.Millisecond)
	after, err := w.root.Read(w.layout.Seat(ops).Store(), 1<<22)
	check(t, err)
	if string(before.Data) != string(after.Data) {
		t.Fatal("the firing wrote though nothing was due on the wall clock")
	}
	if !w.timeoutArmed() || !w.dueAt(timeoutDue(p)) {
		t.Fatal("the timer was not re-armed for the TIMEOUT's wall time")
	}
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs once the wall clock passes = %d", n)
	}
}

// SD4-6: a monotonic timer that slept through a suspend is corrected by the next SetHealth.
func TestSD4_6ASetHealthAfterASuspendReArmsToTheWallTime(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.clk.Suspend(time.Hour)
	w.clk.Advance(0)
	if n := len(timeouts(w.store(ops))); n != 0 {
		t.Fatalf("a TIMEOUT before the timer was corrected: %d", n)
	}
	w.e.SetHealth(ops, team.Health{LastSyncOKMS: w.clk.WallMS()})
	w.clk.Advance(0)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs after SetHealth = %d", n)
	}
}

// fullPending fills s's pending to its cap with HUMAN items, so even a TIMEOUT does not fit.
func fullPending(id paths.TeamID, s *state.Store) {
	for n := range 2048 {
		s.Pending = append(s.Pending, state.Pending{ID: evID(1000 + n), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: startWall, From: alice(id)})
	}
}

// SD4-7: a TIMEOUT write that fails leaves nothing made; a fresh engine over the same store
// makes it once, and a third makes it never again.
func TestSD4_7AFailedTimeoutWriteThenARestartMakesItOnce(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	s := w.pingStore(ops, p)
	fullPending(ops, &s)
	w.heldWith(ops, s)
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	if got := w.store(ops); len(timeouts(got)) != 0 || len(got.Outbox.Pings[0].Reported) != 0 {
		t.Fatalf("a refused write left %+v", got.Outbox.Pings[0])
	}
	w.restart(state.DeliverySocket)
	s = w.pingStore(ops, p)
	check(t, w.root.Replace(w.layout.Seat(ops).Store(), encodeStore(t, s), 0o600))
	w.readd(ops)
	w.clk.Advance(0)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs after the restart = %d", n)
	}
	w.restart(state.DeliverySocket)
	w.readd(ops)
	w.clk.Advance(time.Hour)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs after a second restart = %d", n)
	}
}

// SD4-8: a team that is down, and one stopped by a refused token, still get their TIMEOUTs.
func TestSD4_8ADownOrStoppedTeamStillGetsItsTimeouts(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.heldWith(ops, w.pingStore(ops, outPing(1, startWall, agent(ops, 2))))
	w.heldWith(dev, w.pingStore(dev, outPing(2, startWall, agent(dev, 2))))
	w.e.SetHealth(ops, team.Health{Down: true})
	w.e.SetHealth(dev, team.Health{Stopped: "token"})
	w.advanceTo(startWall + pingTimeoutMS + timeoutGraceMS + 1)
	for _, id := range []paths.TeamID{ops, dev} {
		if n := len(timeouts(w.store(id))); n != 1 {
			t.Fatalf("team %s TIMEOUTs = %d", id, n)
		}
	}
}

// SD4-11: a TIMEOUT write refused by a full pending is tried again 60 s later, logged once,
// and succeeds once room is made.
func TestSD4_11ARefusedTimeoutWriteRetriesEverySixtySeconds(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	s := w.pingStore(ops, p)
	fullPending(ops, &s)
	w.heldWith(ops, s)
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	w.clk.Advance(timeoutRetry)
	if n := len(timeouts(w.store(ops))); n != 0 {
		t.Fatalf("TIMEOUTs into a full pending = %d", n)
	}
	if c := strings.Count(w.log.String(), "the TIMEOUTs due were not written"); c != 1 {
		t.Fatalf("%d log lines for the refused write:\n%s", c, w.log.String())
	}
	w.persistBatch(team.Batch{Team: ops, Redacts: []string{evID(1000)}})
	w.clk.Advance(timeoutRetry - time.Millisecond)
	if n := len(timeouts(w.store(ops))); n != 0 {
		t.Fatal("the retry ran before its 60 s")
	}
	w.clk.Advance(time.Millisecond)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs once room was made = %d", n)
	}
}

// The grace the timer arms for is the one proto.TimeoutDue counts.
func TestTheTimersGraceIsProtosTimeoutDue(t *testing.T) {
	if proto.TimeoutDue(0, timeoutGraceMS) || !proto.TimeoutDue(0, timeoutGraceMS+1) {
		t.Fatalf("timeoutGraceMS %d is not proto's grace", timeoutGraceMS)
	}
}
