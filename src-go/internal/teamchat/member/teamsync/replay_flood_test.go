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
			w.t.Errorf("a batch dropped %d events `rate` (counts %+v)", n, b.Counts)
		}
	}
	if n := w.log.count(": " + proto.ReasonRate); n != 0 {
		w.t.Errorf("the log names %d `rate` drops:\n%s", n, w.log.String())
	}
}

// noReplayCount fails when replayed history counted toward the flood window: a `rate` drop, or
// (a team human being exempt, spec A 11.1) a human post counted or logged over the limit. It is
// read once the team has settled, so a line the judging prints is in the log by then.
func (w *world) noReplayCount(bs []team.Batch) {
	w.t.Helper()
	w.noRateDrop(bs)
	for _, b := range bs {
		if n := b.Counts.HumanOverLimit; n != 0 {
			w.t.Errorf("a batch counted %d human posts over the flood limit (counts %+v)", n, b.Counts)
		}
	}
	if n := w.log.count("over the flood limit"); n != 0 {
		w.t.Errorf("the log names %d posts over the flood limit:\n%s", n, w.log.String())
	}
}

// settled waits until the team has committed the last of bs that a long-poll answered: the team
// long-polls from that batch's position only after the answer's judging is committed and its log
// lines printed. A builder's page carries no position; its commit is before the build's end.
func (w *world) settled(r *running, bs []team.Batch) {
	w.t.Helper()
	next := ""
	for _, b := range bs {
		if b.NextBatch != "" {
			next = b.NextBatch
		}
	}
	if next == "" {
		return
	}
	for {
		select {
		case since := <-r.polls:
			if since == next {
				return
			}
		case err := <-r.done:
			w.t.Fatalf("the team ended before it polled from %q: %v", next, err)
		case <-w.t.Context().Done():
			w.t.Fatalf("the team never polled from %q", next)
		}
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
// own builder, which reads the room from its start (R64). It gives the running team, once its
// build has ended.
func questionsReplay(t *testing.T, n int) (w *world, r *running, old []string, fresh string) {
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
	r = w.startTeam(tm)
	b.ended(t)
	return w, r, old, fresh
}

// R64: a link start's build replays the questions room from its start; the posts its floor
// covers are folded without counting toward the flood window, so none is dropped `rate`, and
// the one new post past the floor is delivered exactly once.
func TestR64AReplayedQuestionsRoomCountsNothingTowardTheFloodWindow(t *testing.T) {
	w, r, old, fresh := questionsReplay(t, -1)
	bs := w.drained()
	w.settled(r, bs)
	w.noReplayCount(bs)
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
	w, _, old, fresh := questionsReplay(t, 3)
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

// humanBurst is a coordinator's link given alice's posts, n of them in one minute of the fake
// clock: the first alone, the rest in one held answer. It gives the batches of both answers and
// the posts' ids, once the held answer's judging is committed (its log lines printed).
func humanBurst(t *testing.T, w *world, n int, post func(i int) string) ([]team.Batch, []string) {
	t.Helper()
	var g *filterGate
	rt := w.readyWith(w.main, func(tm *Team) { g = gateFilter(t, tm, w.filterFor(true)) })
	release := g.armed()
	ids := []string{post(0)}
	first := w.batchWith(func(b team.Batch) bool { return slices.Contains(itemIDs(b), ids[0]) })
	g.isHeld()
	for i := 1; i < n; i++ {
		ids = append(ids, post(i))
	}
	rt.drainPolls()
	release()
	return []team.Batch{first, w.committed(rt.r)}, ids
}

// committed is the batch the team persists for the answer it handles next, given once that
// answer's judging is committed and its log lines printed. Persist hands the batch over before
// the commit, and the team long-polls again only after it, so the test waits for that poll and
// then takes the batch, which is sent by then; a test reads link.log after this, never straight
// after a batch.
func (w *world) committed(r *running) team.Batch {
	w.t.Helper()
	select {
	case <-r.polls:
	case err := <-r.done:
		w.t.Fatalf("the team ended before its next long-poll: %v", err)
	case <-w.t.Context().Done():
		w.t.Fatal("no long-poll after the answer")
	}
	select {
	case b := <-w.eng.batches:
		return b
	default:
		w.t.Fatal("the answer was handled and nothing was persisted")
	}
	return team.Batch{}
}

// checkHumanBurst holds A 11.1's human exemption over a burst: every post delivered once, none
// dropped `rate`, the posts past recv_per_sender_minute counted and each logged.
func checkHumanBurst(t *testing.T, w *world, bs []team.Batch, ids []string) {
	t.Helper()
	w.noRateDrop(bs)
	for _, id := range ids {
		if n := deliveredIn(bs, id); n != 1 {
			t.Errorf("the post %s delivered %d times, want once", id, n)
		}
	}
	over, want := int64(0), int64(len(ids))-w.roster.Limits.RecvPerSenderMinute
	for _, b := range bs {
		over += b.Counts.HumanOverLimit
	}
	if over != want {
		t.Errorf("counted %d human posts over the limit, want %d", over, want)
	}
	if n := w.log.count("over the flood limit"); int64(n) != want {
		t.Errorf("the log names %d posts over the flood limit, want %d:\n%s", n, want, w.log.String())
	}
	for _, id := range ids[len(ids)-int(want):] {
		if w.log.count(id+" over the flood limit") != 1 {
			t.Errorf("no log line names the post %s over the flood limit", id)
		}
	}
}

// A 11.1 (R64's follow-up): a team human is not flood-limited. 70 of a human's posts in the
// questions room in one minute are each delivered to the coordinator once; the 10 past
// recv_per_sender_minute are counted and logged, never dropped.
func TestR64BHumanBurstInQuestionsIsDeliveredAndCounted(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	bs, ids := humanBurst(t, w, w.replayed(), func(i int) string {
		return w.sayIn(w.qroom(), alice, human(fmt.Sprintf("burst %d", i)))
	})
	checkHumanBurst(t, w, bs, ids)
}

// The same in main: 70 posts mentioning the coordinator in one minute.
func TestR64BHumanBurstInMainIsDeliveredAndCounted(t *testing.T) {
	w := newWorld(t)
	bs, ids := humanBurst(t, w, w.replayed(), func(i int) string {
		return w.say(alice, human(fmt.Sprintf("burst %d", i), user(coord)))
	})
	checkHumanBurst(t, w, bs, ids)
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
	r := w.start()
	b := w.batch()
	bs := append([]team.Batch{b}, w.drained()...)
	w.settled(r, bs)
	w.noReplayCount(bs)
	for _, id := range append(old, fresh) {
		if n := deliveredIn(bs, id); n != 1 {
			t.Errorf("the post %s delivered %d times, want once", id, n)
		}
	}
}
