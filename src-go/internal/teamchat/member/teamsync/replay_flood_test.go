package teamsync

import (
	"fmt"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// replayed is past recv_per_sender_minute, so a replay that counted toward the flood window
// would drop the posts past it `rate`.
func (w *world) replayed() int { return int(w.roster.Limits.RecvPerSenderMinute) + 10 }

// drained is every batch the engine holds now, without waiting.
func (w *world) drained() []team.Batch {
	var out []team.Batch
	for {
		select {
		case b := <-w.eng.batches:
			out = append(out, b)
		default:
			return out
		}
	}
}

// noRateDrop fails when a batch counted a `rate` drop or the log names one.
func (w *world) noRateDrop(bs []team.Batch) {
	w.t.Helper()
	for _, b := range bs {
		if n := b.Counts.DroppedBy[proto.ReasonRate]; n != 0 {
			w.t.Errorf("a batch dropped %d events `rate` (counts %+v): replayed history counted toward the flood window", n, b.Counts)
		}
	}
	if n := w.log.count(": " + proto.ReasonRate); n != 0 {
		w.t.Errorf("the log names %d `rate` drops:\n%s", n, w.log.String())
	}
}

// deliveredIn counts the items of id among bs.
func deliveredIn(bs []team.Batch, id string) int {
	n := 0
	for _, b := range bs {
		n += len(slices.DeleteFunc(itemIDs(b), func(got string) bool { return got != id }))
	}
	return n
}

// questionsReplay is a coordinator's questions room holding posts of a human made after its join,
// all inside the room's marker floor, then one new post past it; the team is started with QX1's
// own builder, which reads the room from its start (R64).
func questionsReplay(t *testing.T, n int) (w *world, old []string, fresh string) {
	t.Helper()
	w = newWorld(t)
	if n < 0 {
		n = w.replayed()
	}
	w.asCoordinator()
	w.inQuestions(alice, false)
	w.inQuestions(coord, false)
	w.clk.StepWall(1000)
	old = w.seed(w.qroom(), alice, n)
	check(t, w.client(coord).PutRoomAccountData(t.Context(), user(coord), w.qroom(), paths.FloorAccountData,
		markerContent(state.Floor{MS: w.clk.WallMS()}, w.roster.RestoreEpoch)))
	w.clk.StepWall(1000)
	fresh = w.sayIn(w.qroom(), alice, human("a new post after the restart"))
	tm := w.newTeam(withIndexBuilder())
	b := watchBuilds(tm)
	w.startTeam(tm)
	b.ended(t)
	return w, old, fresh
}

// R64: a link start's build replays the questions room from its start; the posts its floor
// covers are folded without counting toward the flood window, so none is dropped `rate`, and
// the one new post past the floor is delivered exactly once.
func TestR64AReplayedQuestionsRoomCountsNothingTowardTheFloodWindow(t *testing.T) {
	w, old, fresh := questionsReplay(t, -1)
	bs := w.drained()
	w.noRateDrop(bs)
	if n := deliveredIn(bs, fresh); n != 1 {
		t.Errorf("the new post delivered %d times, want once", n)
	}
	for _, id := range old {
		if n := deliveredIn(bs, id); n != 0 {
			t.Errorf("the replayed post %s inside the floor delivered %d times, want none", id, n)
		}
	}
	idx := w.atTheHead(fresh)
	for _, id := range []string{old[0], old[len(old)-1], fresh} {
		if _, ok := idx.Attachment(id); !ok {
			t.Errorf("the post %s is not folded into the index", id)
		}
	}
}

// R64 and QS5: a few replayed posts inside the floor, each a Deliver verdict, are not delivered
// a second time (the floor's skip in qtake), and the new one past it is delivered once.
func TestR64APostInsideTheFloorIsNotDeliveredAgain(t *testing.T) {
	w, old, fresh := questionsReplay(t, 3)
	bs := w.drained()
	for _, id := range old {
		if n := deliveredIn(bs, id); n != 0 {
			t.Errorf("the post %s inside the floor delivered %d times, want none", id, n)
		}
	}
	if n := deliveredIn(bs, fresh); n != 1 {
		t.Errorf("the new post delivered %d times, want once", n)
	}
}

// R64 in main: SY1 (b)'s resume replays main's events after the seat's release time, even those
// its marker covers, as restored items; that replay counts nothing toward the flood window, so
// every restored post and the one new post past the marker are each delivered once.
func TestR64ARestoredMainReplayCountsNothingTowardTheFloodWindow(t *testing.T) {
	w := newWorld(t)
	w.clk.StepWall(1000)
	released := w.clk.WallMS()
	w.clk.StepWall(1000)
	old := make([]string, 0, w.replayed())
	for i := range w.replayed() {
		old = append(old, w.say(alice, human(fmt.Sprintf("restored %d", i), user(me))))
	}
	w.putMarker(markerContent(state.Floor{MS: w.clk.WallMS()}, w.roster.RestoreEpoch))
	w.clk.StepWall(1000)
	fresh := w.say(alice, human("a new post after the restart", user(me)))
	w.place(w.tokens[me], &released)
	w.start()
	b := w.batch()
	bs := append([]team.Batch{b}, w.drained()...)
	w.noRateDrop(bs)
	for _, id := range append(old, fresh) {
		if n := deliveredIn(bs, id); n != 1 {
			t.Errorf("the post %s delivered %d times, want once", id, n)
		}
	}
}
