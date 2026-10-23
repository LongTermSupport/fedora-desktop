package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// statusFile is status.json as the status line reads it, with no flush of a paced write.
func (w *world) statusFile() state.Status {
	w.t.Helper()
	f, err := w.root.Read(w.layout.Run(w.run).Status(), 1<<20)
	check(w.t, err)
	s, err := state.DecodeStatus(f.Data)
	check(w.t, err)
	return s
}

// quiet is a world whose engine wrote status.json for a held ops, then went quiet past the
// pacing, and the wall time of the write a change then makes.
func quietWorld(t *testing.T) (*world, int64) {
	t.Helper()
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	w.held(ops)
	w.clk.Advance(2 * time.Second)
	w.e.SetHealth(ops, team.Health{Role: "worker"})
	at := w.clk.WallMS()
	if st := w.statusFile(); st.WrittenMS != at || st.Teams[0].Role == nil {
		t.Fatalf("a change after a quiet second is not written at once: %+v", st)
	}
	return w, at
}

// SL1, T12: two changes within 300 ms of a write are one write, a second after it.
func TestTwoChangesInASecondAreOneWrite(t *testing.T) {
	w, at := quietWorld(t)
	ops := tid(t, "ops")
	w.clk.Advance(100 * time.Millisecond)
	w.e.SetHealth(ops, team.Health{Role: "worker", Down: true})
	w.clk.Advance(200 * time.Millisecond)
	w.e.SetHealth(ops, team.Health{Role: "coordinator", Down: true})
	w.clk.Advance(699 * time.Millisecond)
	if st := w.statusFile(); st.WrittenMS != at || st.Teams[0].Down {
		t.Fatalf("written within the second: %+v", st)
	}
	w.clk.Advance(time.Millisecond)
	st := w.statusFile()
	if st.WrittenMS != at+1000 || !st.Teams[0].Down || *st.Teams[0].Role != "coordinator" {
		t.Fatalf("the paced write: %+v", st)
	}
	w.clk.Advance(time.Second)
	if again := w.statusFile(); again.WrittenMS != st.WrittenMS {
		t.Fatalf("an unchanged status was written again: %+v", again)
	}
}

// SL1: a quiet held team's status.json is written once a minute, so its written_ms says the link
// answers; a team stopped on a permanent failure is still refreshed.
func TestAQuietHeldTeamIsWrittenOnceAMinute(t *testing.T) {
	w, at := quietWorld(t)
	w.e.SetHealth(tid(t, "ops"), team.Health{Role: "worker", Stopped: exits.CodeToken})
	w.clk.Advance(time.Second)
	at += 1000
	if st := w.statusFile(); st.WrittenMS != at || st.Teams[0].Failed == nil {
		t.Fatalf("the stop: %+v", st)
	}
	for range 3 {
		w.clk.Advance(time.Minute - time.Millisecond)
		if st := w.statusFile(); st.WrittenMS != at {
			t.Fatalf("written before the minute: %+v", st)
		}
		w.clk.Advance(time.Millisecond)
		at += 60_000
		if st := w.statusFile(); st.WrittenMS != at || st.Teams[0].Failed == nil {
			t.Fatalf("the minute's write: %+v", st)
		}
	}
}

// SL1: with no team held status.json is written once at the start, then only on a change.
func TestWithNoTeamStatusJSONIsWrittenOnceThenOnChange(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	w.local(func() {})
	start := w.statusFile()
	if start.WrittenMS != startWall || len(start.Teams) != 0 {
		t.Fatalf("the first write: %+v", start)
	}
	w.clk.Advance(2 * time.Minute)
	if st := w.statusFile(); st.WrittenMS != startWall {
		t.Fatalf("a team-less status.json was refreshed: %+v", st)
	}
	check(t, w.e.SetDesired(t.Context(), state.ReportDesired{State: state.DesiredRefused}))
	if st := w.statusFile(); st.Desired != state.DesiredRefused || st.WrittenMS != w.clk.WallMS() {
		t.Fatalf("the change: %+v", st)
	}
}

// OF1: a down team's row is written beside a running one's, and the minute's refresh goes on.
func TestADownTeamIsWrittenBesideARunningOne(t *testing.T) {
	w, at := quietWorld(t)
	dev := tid(t, "dev")
	w.held(dev)
	w.e.SetHealth(dev, team.Health{Down: true})
	w.clk.Advance(time.Second)
	st := w.statusFile()
	if len(st.Teams) != 2 || !st.Teams[0].Down || st.Teams[1].Down || st.WrittenMS != at+1000 {
		t.Fatalf("rows %+v", st)
	}
	w.clk.Advance(time.Minute)
	if again := w.statusFile(); again.WrittenMS != at+61_000 || !again.Teams[0].Down {
		t.Fatalf("the refresh: %+v", again)
	}
}

// SL1: a write that fails is logged once and not retried each second; the minute's refresh
// writes it once status.json can be written again.
func TestAFailedWriteIsRetriedByTheMinutesRefresh(t *testing.T) {
	w, at := quietWorld(t)
	run := w.layout.Run(w.run).Dir()
	check(t, w.root.RemoveTree(run))
	w.clk.Advance(time.Second)
	w.e.SetHealth(tid(t, "ops"), team.Health{Role: "coordinator"})
	w.clk.Advance(30 * time.Second)
	if n := strings.Count(w.log.String(), "status.json not written"); n != 1 {
		t.Fatalf("%d failed writes logged: %q", n, w.log.String())
	}
	check(t, w.root.MkdirAll(run))
	w.clk.Advance(30 * time.Second)
	if st := w.statusFile(); st.WrittenMS != at+61_000 || *st.Teams[0].Role != "coordinator" {
		t.Fatalf("the refresh: %+v", st)
	}
}

// A timer that fires after the state goroutine has ended runs nothing.
func TestATimerAfterTheEndRunsNothing(t *testing.T) {
	e := &Engine{ops: make(chan func()), done: make(chan struct{})}
	close(e.done)
	e.onGoroutine(func() { t.Fatal("ran on an ended goroutine") })
}
