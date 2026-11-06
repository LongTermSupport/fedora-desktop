package teamsync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// readyTeam is a coordinator's team with its index at the head, as readyWith leaves it.
type readyTeam struct {
	tm      *Team
	r       *running
	fb      *fakeBuilder
	filters <-chan json.RawMessage
}

// readyWith is the coordinator's team, prep applied before it runs, with an empty index at the
// head whose last folded event is last, at its first long-poll with the coordinator filter.
func (w *world) readyWith(last string, prep func(*Team)) readyTeam {
	w.t.Helper()
	w.asCoordinator()
	fb := &fakeBuilder{}
	tm := w.newTeam(withBuilder(fb))
	filters := filtersOf(tm)
	if prep != nil {
		prep(tm)
	}
	r := w.startTeam(tm)
	w.batch()
	r.poll(w.t)
	tm.IndexAtHead(proto.NewQuestionIndex(server, 0), last)
	for coordinator := w.filterFor(true); nextFilter(w.t, filters) != coordinator; {
		continue
	}
	// What is posted next is later than the join, the room's floor (QS5), on the fake clock.
	w.clk.Advance(time.Second)
	return readyTeam{tm: tm, r: r, fb: fb, filters: filters}
}

// filterGate holds the team's next request with the filter want, once armed, just before it is
// made, until released: the posts made meanwhile reach that request at once, so a room's
// timeline in its answer is limited.
type filterGate struct {
	t    *testing.T
	arm  chan chan struct{}
	held chan struct{}
}

func gateFilter(t *testing.T, tm *Team, want string) *filterGate {
	t.Helper()
	g := &filterGate{t: t, arm: make(chan chan struct{}, 1), held: make(chan struct{}, 1)}
	announce := tm.onFilter
	tm.onFilter = func(f json.RawMessage) {
		// Armed or not is read before the request is announced: a test that arms the gate once
		// it has seen the announcement means the request after this one, and one read after the
		// announcement could be the arm of that test, which holds the request that would carry
		// the very post the test then waits for.
		var rel chan struct{}
		if string(f) == want {
			select {
			case rel = <-g.arm:
			default:
			}
		}
		announce(f)
		if rel == nil {
			return
		}
		g.held <- struct{}{}
		select {
		case <-rel:
		case <-t.Context().Done():
		}
	}
	return g
}

// armed holds the next request of the gate's filter; the release lets it go.
func (g *filterGate) armed() func() {
	rel := make(chan struct{})
	g.arm <- rel
	return func() { close(rel) }
}

// isHeld returns once the armed request is held.
func (g *filterGate) isHeld() {
	g.t.Helper()
	select {
	case <-g.held:
	case <-g.t.Context().Done():
		g.t.Fatal("the request was never held")
	}
}

// drainPolls forgets the long-polls and filters the team has reported so far.
func (rt readyTeam) drainPolls() {
	for {
		select {
		case <-rt.r.polls:
		case <-rt.filters:
		default:
			return
		}
	}
}

// persistedBefore is the batch the team persists for the answer it handles next, failing fast
// when the team reaches its next long-poll with none (the batch, sent before Persist returns,
// is looked for once more first).
func (w *world) persistedBefore(r *running) team.Batch {
	w.t.Helper()
	select {
	case b := <-w.eng.batches:
		return b
	case <-r.polls:
		select {
		case b := <-w.eng.batches:
			return b
		default:
		}
		w.t.Fatal("the answer was handled and nothing was persisted")
	case <-w.t.Context().Done():
		w.t.Fatal("no batch")
	}
	return team.Batch{}
}

// More posts than the long-poll's timeline limit (100) make a limited timeline.
const overLimit = 105

// questionPosts posts overLimit of alice's human posts in questions, and gives their ids oldest
// first.
func (w *world) questionPosts() []string {
	w.t.Helper()
	ids := make([]string, 0, overLimit)
	for i := range overLimit {
		ids = append(ids, w.sayIn(w.qroom(), alice, human(fmt.Sprintf("question chatter %d", i))))
	}
	return ids
}

// keyIndex is the index a batch's questions key holds.
func keyIndex(t *testing.T, b team.Batch) *proto.QuestionIndex {
	t.Helper()
	k := b.Questions
	if k == nil || k.Index == nil {
		t.Fatalf("batch %+v holds no questions key", b)
	}
	idx, err := proto.LoadQuestionIndex(server, 0, *k.Index)
	check(t, err)
	return idx
}

// QS7: a limited questions timeline after an answer whose fold was written (the chain) is
// filled back within SY3's bound, so every event in the gap is folded, and the key is written
// at the answer's position.
func TestQS7ALimitedTimelineIsFilledToTheLastFoldedEvent(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	rt := w.readyWith(w.main, func(tm *Team) { g = gateFilter(t, tm, w.filterFor(true)) })
	release := g.armed()
	first := w.sayIn(w.qroom(), alice, human("the first"))
	w.batchWith(lastIs(first))
	g.isHeld()
	posts := w.questionPosts()
	rt.drainPolls()
	release()
	b := w.persistedBefore(rt.r)
	if k := b.Questions; k == nil || k.Last == nil || *k.Last != posts[len(posts)-1] || *k.Token != b.NextBatch {
		t.Fatalf("the key %+v: the answer's fold was not written at its position", k)
	}
	idx := keyIndex(t, b)
	for _, id := range []string{first, posts[0], posts[4]} {
		if _, seen := idx.Attachment(id); !seen {
			t.Errorf("the post %s, in the filled gap or before it, is not in the index", id)
		}
	}
	if jobs := rt.fb.jobs(); len(jobs) != 1 {
		t.Errorf("builder jobs %+v: the gap was within the bound, so the start's build is the only one", jobs)
	}
}

// QS7: with no chain (the index handed over by the builder), the fill goes on until the index's
// last folded event is found, and only what follows it is folded.
func TestQS7WithNoChainTheFillFindsTheLastFoldedEvent(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	before := w.sayIn(w.qroom(), alice, human("folded by the build"))
	var g *filterGate
	var release func()
	rt := w.readyWith(before, func(tm *Team) {
		g = gateFilter(t, tm, w.filterFor(true))
		release = g.armed()
	})
	g.isHeld()
	posts := w.questionPosts()
	rt.drainPolls()
	release()
	b := w.persistedBefore(rt.r)
	if k := b.Questions; k == nil || k.Last == nil || *k.Last != posts[len(posts)-1] {
		t.Fatalf("the key %+v: the answer's fold was not written", k)
	}
	idx := keyIndex(t, b)
	if _, seen := idx.Attachment(posts[0]); !seen {
		t.Error("the oldest post of the gap is not in the index")
	}
	if _, seen := idx.Attachment(before); seen {
		t.Error("the index's last folded event was folded again: the fill kept what precedes the event it found")
	}
}

// catchUp is the assertion QS7 makes of an answer whose gap the fill did not close: main's item
// delivered, nothing of questions taken, the key left as it was, the builder given the store's
// key, and the room out of the next request's filter.
func (w *world) catchUp(rt readyTeam, b team.Batch, mainID string, key *proto.QuestionsState) {
	w.t.Helper()
	if got := itemIDs(b); !slices.Equal(got, []string{mainID}) {
		w.t.Errorf("items %v, want main's item alone", got)
	}
	if b.Questions != nil {
		w.t.Errorf("the key %+v written by an answer whose gap was not filled", b.Questions)
	}
	if _, ok := b.Floors[proto.PurposeQuestions]; ok {
		w.t.Errorf("floors %v: the questions floor moved", b.Floors)
	}
	// The first job is the build the team's start began (the store held no key then).
	jobs := rt.fb.jobs()
	if len(jobs) != 2 || jobs[1].RoomID != w.qroom() || !reflect.DeepEqual(jobs[1].Key, key) {
		w.t.Fatalf("builder jobs %+v, want the start's build, then one catch-up from the store's key %+v", jobs, key)
	}
	if st := rt.tm.QuestionsStatus(); st.State != team.QuestionsBuilding {
		w.t.Errorf("status %+v, want building", st)
	}
	// The filters were drained at the release: this is the next request's.
	if got := nextFilter(w.t, rt.filters); got != w.filterFor(false) {
		w.t.Errorf("the request after the gap:\n%s\nwant the worker form", got)
	}
}

// QS7: past SY3's page bound the index cannot skip events: the answer's questions events are
// not taken, main's are persisted, the questions key stays as it was (n3f-D19), QX1's
// catch-up is begun from the store's key, and the room leaves the filter.
func TestQS7PastTheBoundTheRoomIsCaughtUp(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	rt := w.readyWith(w.main, func(tm *Team) {
		g = gateFilter(t, tm, w.filterFor(true))
		tm.pages = pageBound{size: 2, max: 2}
	})
	release := g.armed()
	first := w.sayIn(w.qroom(), alice, human("the first"))
	w.batchWith(lastIs(first))
	g.isHeld()
	store, err := w.eng.Load(w.team, user(coord))
	check(t, err)
	w.questionPosts()
	mainID := w.agentText(peer, "status?", user(coord))
	rt.drainPolls()
	release()
	w.catchUp(rt, w.persistedBefore(rt.r), mainID, store.Questions)
}

// QS7: a fill that reaches the room's start without the index's last folded event cannot place
// the gap either: it is the catch-up's.
func TestQS7TheLastFoldedEventNotFoundIsCaughtUp(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	var release func()
	rt := w.readyWith("$not-in-the-room", func(tm *Team) {
		g = gateFilter(t, tm, w.filterFor(true))
		release = g.armed()
	})
	g.isHeld()
	store, err := w.eng.Load(w.team, user(coord))
	check(t, err)
	w.questionPosts()
	mainID := w.agentText(peer, "status?", user(coord))
	rt.drainPolls()
	release()
	w.catchUp(rt, w.persistedBefore(rt.r), mainID, store.Questions)
}

// QS7 and n3f-D8: one sync token covers both rooms, so a questions fill that fails transiently
// holds the whole answer: nothing is persisted, and the team backs off.
func TestQS7ATransientFillFailureHoldsTheWholeAnswer(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	var release func()
	rt := w.readyWith(w.main, func(tm *Team) {
		g = gateFilter(t, tm, w.filterFor(true))
		release = g.armed()
	})
	g.isHeld()
	w.questionPosts()
	w.agentText(peer, "status?", user(coord))
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/messages", Status: http.StatusBadGateway, Body: []byte(`{}`)})
	release()
	select {
	case d := <-rt.r.waits:
		if d != time.Second {
			t.Errorf("backoff %v, want the first wait", d)
		}
	case b := <-w.eng.batches:
		t.Fatalf("the answer was persisted (%+v) though its questions fill failed", b)
	case <-t.Context().Done():
		t.Fatal("no backoff")
	}
	w.noBatch()
}
