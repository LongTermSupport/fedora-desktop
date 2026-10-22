package teamsync

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// markerPuts counts the PUTs of the floor marker fakehs received.
func (w *world) markerPuts() int {
	n := 0
	for _, r := range w.srv.Requests() {
		if r.Method == http.MethodPut && strings.Contains(r.Path, paths.FloorAccountData) {
			n++
		}
	}
	return n
}

// marker is the floor the member's marker holds now; -1 for none.
func (w *world) marker() int64 {
	w.t.Helper()
	raw, found, err := w.client(me).RoomAccountData(w.t.Context(), user(me), w.main, paths.FloorAccountData)
	check(w.t, err)
	if !found {
		return -1
	}
	floor, ok := decodeMarker(raw)
	if !ok {
		w.t.Fatalf("marker %s", raw)
	}
	return floor
}

// SY10: a moved floor is put at once the first time, then at most once per 10 s of boot time.
func TestTheFloorMarkerIsCoalesced(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	first := w.quiet("one")
	w.batch()
	r.poll(t)
	if f := w.marker(); f != w.ts(first) || w.markerPuts() != 1 {
		t.Fatalf("marker %d, %d puts", f, w.markerPuts())
	}
	w.quiet("two")
	w.batch()
	r.poll(t)
	if n := w.markerPuts(); n != 1 {
		t.Errorf("%d puts within 10 s", n)
	}
	w.clk.Advance(floorPutGap)
	third := w.quiet("three")
	w.batch()
	r.poll(t)
	if f := w.marker(); f != w.ts(third) || w.markerPuts() != 2 {
		t.Errorf("marker %d, %d puts", f, w.markerPuts())
	}
}

// SY10, A2: a session that starts from a store floor the marker may not hold puts it after its
// first answer, though the floor did not move in this session (the last link may have ended,
// or been killed, before its coalesced put).
func TestASessionPutsTheFloorItStartsFrom(t *testing.T) {
	w := newWorld(t)
	q := w.quiet("before the restart")
	w.eng.preload(user(me), runID(t, linkRun), "", w.roster.RestoreEpoch)
	w.eng.mu.Lock()
	w.eng.store.Sync.FloorMS = map[string]int64{proto.PurposeMain: w.ts(q)}
	w.eng.mu.Unlock()
	r := w.start()
	w.batch()
	r.poll(t)
	if f := w.marker(); f != w.ts(q) || w.markerPuts() != 1 {
		t.Fatalf("marker %d, %d puts", f, w.markerPuts())
	}
}

// SY10, A2: a floor moved down (an owed item at or below the last high-water, after a clock
// step) is put at once, never coalesced, so a seat taken with no store cannot cut that item.
func TestAFloorMovedDownIsPutAtOnce(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	first := w.quiet("one")
	w.batch()
	r.poll(t)
	if w.markerPuts() != 1 {
		t.Fatalf("%d puts", w.markerPuts())
	}
	w.clk.StepWall(-500)
	owed := w.say(alice, human("for me", user(me)))
	if w.ts(owed) > w.ts(first) {
		t.Fatalf("the step did not move the clock back: %d after %d", w.ts(owed), w.ts(first))
	}
	w.batch()
	r.poll(t)
	if f := w.marker(); f != w.ts(owed)-1 || w.markerPuts() != 2 {
		t.Fatalf("marker %d (want %d), %d puts", f, w.ts(owed)-1, w.markerPuts())
	}
}

// SY10, A4: a floor the engine raised outside a sync answer is the marker's next put; one that
// is not above the floor known, or with no floor known, changes nothing.
func TestAFloorRaisedIsTheMarkersNext(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	tm.FloorRaised(5)
	check(t, tm.FlushFloor(t.Context()))
	if n := w.markerPuts(); n != 0 {
		t.Fatalf("%d puts with no floor known", n)
	}
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	owed := w.say(alice, human("for me", user(me)))
	w.batch()
	r.poll(t)
	if f := w.marker(); f != w.ts(owed)-1 {
		t.Fatalf("marker %d", f)
	}
	tm.FloorRaised(w.ts(owed) - 5)
	tm.FloorRaised(w.ts(owed))
	check(t, tm.FlushFloor(t.Context()))
	if f := w.marker(); f != w.ts(owed) {
		t.Fatalf("marker %d, want %d", f, w.ts(owed))
	}
}

// SY10: FlushFloor puts the floor at once; with none known it puts nothing; a failed put is one
// line.
func TestFlushFloor(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	check(t, tm.FlushFloor(t.Context()))
	if n := w.markerPuts(); n != 0 {
		t.Errorf("%d puts with no floor", n)
	}
	r := w.startTeam(tm)
	w.batch()
	r.poll(t)
	w.quiet("one")
	w.batch()
	r.poll(t)
	second := w.quiet("two")
	w.batch()
	r.poll(t)
	check(t, tm.FlushFloor(t.Context()))
	if f := w.marker(); f != w.ts(second) {
		t.Errorf("marker %d", f)
	}
	w.srv.Inject(fakehs.Fault{Method: http.MethodPut, Path: paths.FloorAccountData, Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 3})
	if err := tm.FlushFloor(t.Context()); err == nil {
		t.Error("a refused put was taken")
	}
	w.clk.Advance(floorPutGap)
	w.quiet("three")
	w.batch()
	r.poll(t)
	if n := w.log.count("was not put"); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
}

// SY12: the detector's two thresholds, each just below and just past.
func TestTheFreezeDetector(t *testing.T) {
	clk := clock.NewFake(time.Hour, startMS)
	f := freezeDetector{clk: clk}
	if _, frozen := f.atStart(); frozen {
		t.Error("the first reading is a freeze")
	}
	clk.Suspend(maxSkewGrowth)
	if _, frozen := f.atStart(); frozen {
		t.Error("2 s is a freeze")
	}
	clk.Suspend(maxSkewGrowth + time.Millisecond)
	if g, frozen := f.atStart(); !frozen || g != maxSkewGrowth+time.Millisecond {
		t.Errorf("growth %v, frozen %v", g, frozen)
	}
	started := clk.Boot()
	clk.Advance(requestDeadline + maxOverdue)
	if _, frozen := f.atAnswer(started); frozen {
		t.Error("10 s past the deadline is a freeze")
	}
	clk.Advance(time.Millisecond)
	if over, frozen := f.atAnswer(started); !frozen || over != maxOverdue+time.Millisecond {
		t.Errorf("over %v, frozen %v", over, frozen)
	}
	clk.Suspend(3 * time.Second)
	if g, frozen := f.atAnswer(clk.Boot()); !frozen || g != 3*time.Second {
		t.Errorf("growth %v, frozen %v", g, frozen)
	}
}

// SY12: a suspend during a long-poll drops the connection, says so once and starts the session
// again; the answer is discarded and judged again from the same position.
func TestASuspendRestartsTheSession(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	since := w.batch().NextBatch
	r.poll(t)
	w.clk.Suspend(5 * time.Second)
	id := w.say(alice, human("after the suspend", user(me)))
	if got := r.poll(t); got != since {
		t.Errorf("after the freeze the team polls from %q, want %q", got, since)
	}
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
	if n := w.log.count("a freeze or suspend of 5s"); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
}

// SY12: a suspend between two long-polls (here, during the store write) is caught at the next
// one's start.
func TestASuspendBetweenPolls(t *testing.T) {
	w := newWorld(t)
	writes := 0
	w.eng.setPersistErr(func(team.Batch) error {
		if writes++; writes == 2 {
			w.clk.Suspend(5 * time.Second)
		}
		return nil
	})
	r := w.start()
	w.batch()
	r.poll(t)
	w.say(alice, human("one", user(me)))
	since := w.batch().NextBatch
	if got := r.poll(t); got != since {
		t.Errorf("polls from %q, want %q", got, since)
	}
	if n := w.log.count("a freeze or suspend"); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
}

// B 2.3.2: the marker carries the store's room floor, just below the oldest item still owed, not
// the batch's high-water; a later batch with nothing owed older leaves it where the store has it.
func TestTheMarkerStaysBelowAnOwedItem(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	id := w.say(alice, human("hello", user(me)))
	b := w.batch()
	r.poll(t)
	owed := b.Items[0].Pending.SentMS
	if f := w.marker(); f != owed-1 || w.ts(id) != owed {
		t.Fatalf("marker %d with an item owed at %d", f, owed)
	}
	w.clk.Advance(floorPutGap)
	w.quiet("after")
	w.batch()
	r.poll(t)
	if f := w.marker(); f != owed-1 {
		t.Errorf("marker %d moved past the owed item at %d", f, owed)
	}
}
