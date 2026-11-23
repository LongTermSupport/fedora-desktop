package engine

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// fakeQuestions is the questions room of a fake team: nil idx is a member that does not
// coordinate it. mu makes its composes one at a time (QX4), as the team's judging hold does.
type fakeQuestions struct {
	mu       sync.Mutex
	self     string
	idx      *proto.QuestionIndex
	posted   []string
	postable error
	events   map[string]json.RawMessage
	composes int
	checks   int
}

func (f *fakeTeam) Questions(_ context.Context, compose func(team.QuestionsView) ([]team.Placement, error)) (team.QuestionsView, error) {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	if f.q.idx == nil {
		return team.QuestionsView{}, exits.New(exits.CodeRole, nil, "not the coordinator")
	}
	f.q.composes++
	ps, err := compose(team.QuestionsView{Room: qRoom, Index: f.q.idx.Copy()})
	if err != nil {
		return team.QuestionsView{}, err
	}
	for _, p := range ps {
		switch {
		case p.Question != nil:
			err = f.q.idx.PlaceQuestion(p.Txn, f.q.self, *p.Question, p.Body)
		case p.Thread != nil:
			err = f.q.idx.PlaceThread(p.Txn, f.q.self, *p.Thread)
		}
		if err != nil {
			return team.QuestionsView{}, err
		}
	}
	return team.QuestionsView{Room: qRoom, Index: f.q.idx.Copy()}, nil
}

func (f *fakeTeam) QuestionPosted(_, id string) {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	f.q.posted = append(f.q.posted, id)
}

func (f *fakeTeam) QuestionsPostable(proto.Value) error {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	f.q.checks++
	return f.q.postable
}

func (f *fakeTeam) FetchEvent(_ context.Context, id string) (json.RawMessage, error) {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	if raw, ok := f.q.events[id]; ok {
		return raw, nil
	}
	return nil, exits.New(exits.CodeNotFound, nil, "not in questions")
}

func (f *fakeTeam) postedQuestions() []string {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	return slices.Clone(f.q.posted)
}

func (f *fakeTeam) postableChecks() int {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	return f.q.checks
}

func (f *fakeTeam) setPostable(err error) {
	f.q.mu.Lock()
	defer f.q.mu.Unlock()
	f.q.postable = err
}

const askBody = "Which licence for the docs?\nContext: the site goes public.\nQuestion: MIT or CC-BY?\nWhy a human: decision on the licence"

// queuedQuestion is a store of team id, this member the coordinator, holding a question Q1 in
// `sending`, not yet tried.
func (w *world) queuedQuestion(id paths.TeamID) state.Store {
	w.t.Helper()
	out, err := proto.Composer{Server: paths.ServerName(id), Self: agent(id, 1), Roster: coordinatorRoster(id), Questions: proto.NewQuestionIndex(paths.ServerName(id), 0)}.
		Question(proto.QuestionPost{Text: askBody})
	check(w.t, err)
	s := w.newStore(id)
	txn := "tc0123456789abcdef0123456789abcdef"
	s.Outbox.Sending = []state.Sending{{Txn: txn, Room: proto.PurposeQuestions, Kind: state.SendQuestion, Type: proto.MessageType, Content: out.Content.Compact(), SinceMS: startWall}}
	return s
}

// QX3: a queued question is posted to the questions room; the write that records it sets
// questions_unechoed to its event, and the team is told the post is this run's.
func TestQX3AQueuedQuestionIsPostedAndAwaitsItsEcho(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.queuedQuestion(ops)
	f := w.sendingUnder(ops, s, coordinatorRoster(ops))
	w.until(w.sendsLeft(ops))
	if got := f.notices.sentRooms(); !slices.Equal(got, []string{qRoom}) {
		t.Fatalf("the question went to %v, want the questions room; log %s", got, w.log.String())
	}
	st := w.store(ops)
	posted := f.postedQuestions()
	if len(st.Outbox.Sending) != 0 || st.Outbox.QuestionsUnechoed == nil || len(posted) != 1 || posted[0] != *st.Outbox.QuestionsUnechoed {
		t.Errorf("sending %d, unechoed %v, told %v: want the post recorded unechoed and told", len(st.Outbox.Sending), st.Outbox.QuestionsUnechoed, posted)
	}
	if st.Outbox.LastSent != nil {
		t.Error("a questions post moved main's recovery anchor")
	}
}

// QX3: a queued question waits while the index is not ready, and is dropped when its number is
// counted by another's question.
func TestQX3AQueuedQuestionWaitsForTheIndexAndIsDroppedWhenItsNumberIsTaken(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.queuedQuestion(ops)
	f := w.fake(ops)
	f.setPostable(team.ErrQuestionsNotReady)
	f = w.sendingUnder(ops, s, coordinatorRoster(ops))
	waitFor(w, func() bool { return f.postableChecks() > 0 })
	if len(f.notices.sent()) != 0 || len(w.store(ops).Outbox.Sending) != 1 {
		t.Fatal("a question was posted while the index was not ready")
	}
	f.setPostable(&proto.RefusalError{Reason: proto.ReasonTarget, Detail: "Q1 is another question now"})
	w.clk.Advance(retryHold)
	w.until(w.sendsLeft(ops))
	if len(f.notices.sent()) != 0 || len(w.store(ops).Outbox.Sending) != 0 {
		t.Errorf("a question whose number is taken: sent %v, sending %d, want none of either", f.notices.sent(), len(w.store(ops).Outbox.Sending))
	}
}

// QX3's hook line: the uncounted posts a written batch folded are owed to the hooks with their
// numbers; a line told raises `said` in the store, so it is never owed again; a count above
// `said` with no number known (a restart) is owed as posts whose numbers are unknown.
func TestUncountedPostsAreOwedToTheHooksUntilSaid(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Outbox.QuestionsUncounted = state.Uncounted{Count: 1}
	w.add(ops, encodeStore(t, s))
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s1", Questions: questionsKey("s1"), QuestionsUncounted: []proto.Dropped{{Txn: "t1", N: 4}, {Txn: "t2", N: 5, Thread: true}}})
	ht := w.hookTeam(ops)
	if ht.UncountedUnknown != 1 || !slices.Equal(ht.Uncounted, []proto.Dropped{{Txn: "t1", N: 4}, {Txn: "t2", N: 5, Thread: true}}) {
		t.Fatalf("owed %+v and %d unknown, want Q4 and the thread post Q5, and 1 unknown", ht.Uncounted, ht.UncountedUnknown)
	}
	check(t, w.e.UncountedSaid(t.Context(), ops, 2))
	if u := w.store(ops).Outbox.QuestionsUncounted; u != (state.Uncounted{Count: 3, Said: 2}) {
		t.Errorf("stored %+v, want 3 counted and 2 said", u)
	}
	if ht := w.hookTeam(ops); ht.UncountedUnknown != 0 || !slices.Equal(ht.Uncounted, []proto.Dropped{{Txn: "t2", N: 5, Thread: true}}) {
		t.Errorf("owed %+v and %d unknown after two said, want the thread post Q5 alone", ht.Uncounted, ht.UncountedUnknown)
	}
}

// hookTeam is the hooks' view of team id.
func (w *world) hookTeam(id paths.TeamID) HookTeam {
	w.t.Helper()
	v, err := w.e.HookView(w.t.Context())
	check(w.t, err)
	for _, ht := range v.Teams {
		if ht.Team == id {
			return ht
		}
	}
	w.t.Fatalf("team %s is not in the hooks' view", id)
	return HookTeam{}
}

// QX3: the write that folds the own post's echo sets questions_unechoed back to null and counts
// an uncounted echo; a write that folds other events leaves it.
func TestPersistClearsTheUnechoedPostWhenItsEchoIsFolded(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	unechoed := evID(77)
	s.Outbox.QuestionsUnechoed = &unechoed
	w.add(ops, encodeStore(t, s))
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s1", Questions: questionsKey("s1"), QuestionsFolded: []string{evID(76)}})
	if w.store(ops).Outbox.QuestionsUnechoed == nil {
		t.Fatal("another event's fold cleared the unechoed post")
	}
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s2", Questions: questionsKey("s2"), QuestionsFolded: []string{unechoed}, QuestionsUncounted: []proto.Dropped{{Txn: "t1", N: 1}}})
	if o := w.store(ops).Outbox; o.QuestionsUnechoed != nil || o.QuestionsUncounted.Count != 1 {
		t.Errorf("unechoed %v, uncounted %+v: want null and 1", o.QuestionsUnechoed, o.QuestionsUncounted)
	}
}
