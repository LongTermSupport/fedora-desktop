package teamsync

import (
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// coordMarker is the floor the coordinator's marker holds in room now; -1 for none.
func (w *world) coordMarker(room string) int64 {
	w.t.Helper()
	raw, found, err := w.client(coord).RoomAccountData(w.t.Context(), user(coord), room, paths.FloorAccountData)
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

// QS5, SY10: the marker is per room. A questions floor moved by an answer is put in the
// questions room, as the store keeps it (just below the item it owes, B 2.3.2), and main's
// marker is not given it.
func TestQS5AQuestionsFloorMoveIsMarkedInTheQuestionsRoom(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	mainBefore := w.coordMarker(w.main)
	// The long-poll readyWith left is forgotten before the post, not after it: the post ends that
	// poll and the team makes its next at once, which the poll below waits for and a drain after
	// the post could take with the first.
	rt.drainPolls()
	id := w.sayIn(w.qroom(), alice, human("is the build green?"))
	b := w.batchWith(lastIs(id))
	rt.r.poll(t)
	if len(b.Items) != 1 || b.Items[0].Pending.ID != id {
		t.Fatalf("items %v, want the post", itemIDs(b))
	}
	if got, want := w.coordMarker(w.qroom()), b.Items[0].Pending.SentMS-1; got != want {
		t.Errorf("the questions room's marker %d, want %d (just below the owed post)", got, want)
	}
	if got := w.coordMarker(w.main); got != mainBefore {
		t.Errorf("main's marker moved from %d to %d on a questions floor move", mainBefore, got)
	}
	// Within floorPutGap a move is coalesced; FlushFloor puts it at once.
	// The first post handed and gone, the next one is the room's oldest owed.
	w.eng.withStore(func(s *state.Store) { s.Pending = nil })
	w.clk.StepWall(1000)
	next := w.sayIn(w.qroom(), alice, human("and the tests?"))
	w.batchWith(lastIs(next))
	rt.r.poll(t)
	store, err := w.eng.Load(w.team, user(coord))
	check(t, err)
	moved := store.Sync.FloorMS[proto.PurposeQuestions]
	if got := w.coordMarker(w.qroom()); got == moved {
		t.Fatalf("marker %d: the second move within %v was not coalesced", got, floorPutGap)
	}
	check(t, rt.tm.FlushFloor(t.Context()))
	if got := w.coordMarker(w.qroom()); got != moved {
		t.Errorf("after FlushFloor the questions room's marker %d, want %d", got, moved)
	}
}

// QS5: with no store floor, a member that joined the room earlier resumes its items from the
// earlier of its questions marker and released_ms, from either alone when only one exists, and
// never from before its own join.
func TestQS5WithNoStoreFloorTheBuildResumesFromTheEarlierOfMarkerAndRelease(t *testing.T) {
	cases := []struct {
		name             string
		marker, released int64 // ms before now; 0 for none
		wantMarker       bool
	}{
		{name: "the marker earlier", marker: 2000, released: 1000, wantMarker: true},
		{name: "the release earlier", marker: 1000, released: 2000},
		{name: "the marker alone", marker: 1500, wantMarker: true},
		{name: "the release alone", released: 1500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.inQuestions(coord, true)
			// The bounds are later than the join, so the join is not the floor.
			w.clk.StepWall((10 * time.Second).Milliseconds())
			now := w.clk.WallMS()
			if c.marker > 0 {
				check(t, w.client(coord).PutRoomAccountData(t.Context(), user(coord), w.qroom(), paths.FloorAccountData,
					markerContent(now-c.marker, w.roster.RestoreEpoch)))
			}
			w.asCoordinator()
			if c.released > 0 {
				rel := now - c.released
				w.place(w.tokens[coord], &rel)
			}
			fb := &fakeBuilder{}
			r := w.startTeam(w.newTeam(withBuilder(fb)))
			w.batch()
			r.poll(t)
			jobs := fb.jobs()
			if len(jobs) != 1 {
				t.Fatalf("builder jobs %+v, want the start's build", jobs)
			}
			want := now - c.released
			if c.wantMarker {
				want = now - c.marker
			}
			if jobs[0].Floor != want {
				t.Errorf("the build's floor %d, want %d", jobs[0].Floor, want)
			}
		})
	}
}
