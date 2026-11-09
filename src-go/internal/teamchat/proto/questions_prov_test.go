package proto

import (
	"errors"
	"testing"
)

func TestQuestionIndexProvisionalEntries(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	other := "Head\nContext: c\nQuestion: Another thing?"
	n := r.index.Next()
	if err := r.index.PlaceQuestion("txn-2", coordID, Question{N: n, RemindAfterS: 3600}, QuestionName(int(n))+" "+other); err != nil {
		t.Fatal(err)
	}
	if r.index.Next() != 3 || r.index.Open() != 2 {
		t.Errorf("after placing: Next() %d, open %d", r.index.Next(), r.index.Open())
	}
	q := mustQ(t, r.index, 2)
	if !q.Provisional || q.Headline != "Head" || q.EventID != "" {
		t.Fatalf("Q2 = %+v", q)
	}
	if dup, found := r.index.OpenDuplicate("Another thing?"); !found || dup != 2 {
		t.Errorf("the next ask does not see the placed question as a duplicate: %d %v", dup, found)
	}
	if err := r.index.PlaceQuestion("txn-x", coordID, Question{N: 7}, QuestionName(7)+" "+other); !errors.Is(err, ErrNotPlaced) {
		t.Errorf("a question placed at a number that is not Next(): %v", err)
	}
	if len(r.index.State().Questions) != 1 || len(r.index.Reminders(testNow+9e9)) != 0 {
		t.Error("a provisional entry was persisted or reminded")
	}
	r.echoOfQ2(t, other)
}

func (r *questionRoom) echoOfQ2(t *testing.T, other string) {
	t.Helper()
	r.post(qid('b'), coordID, 8000, questionPost(2, other, ""))
	if q := mustQ(t, r.index, 2); q.Provisional || q.EventID != qid('b') || r.index.Next() != 3 || r.index.Open() != 2 {
		t.Errorf("after the echo: %+v, Next() %d, open %d", q, r.index.Next(), r.index.Open())
	}
	if got := r.index.TakeDropped(); len(got) != 0 {
		t.Errorf("dropped after a counted echo: %v", got)
	}
}

func TestQuestionIndexProvisionalEntryDroppedWhenItsEchoIsNotCounted(t *testing.T) {
	r := newQuestionRoom(t, 0)
	n := r.index.Next()
	if err := r.index.PlaceQuestion("txn-1", coordID, Question{N: n}, questionBody(n)); err != nil {
		t.Fatal(err)
	}
	r.post(qid('x'), adminID, 9000, questionPost(1, goodQuestion, ""))
	if q := mustQ(t, r.index, 1); q.Provisional || q.EventID != qid('x') {
		t.Fatalf("the admin's question does not hold Q1: %+v", q)
	}
	if got := r.index.TakeDropped(); len(got) != 0 {
		t.Fatalf("dropped before its own echo: %v", got)
	}
	r.post(qid('y'), coordID, 8000, questionPost(1, goodQuestion, ""))
	if got := r.index.TakeDropped(); len(got) != 1 || got[0] != "txn-1" {
		t.Errorf("TakeDropped() = %v, want [txn-1]", got)
	}
	if got := r.index.TakeDropped(); len(got) != 0 {
		t.Errorf("TakeDropped() again = %v", got)
	}
	if q := mustQ(t, r.index, 1); q.EventID != qid('x') {
		t.Errorf("Q1 = %+v", q)
	}
}

func TestQuestionIndexProvisionalThreadPosts(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('u'), aliceID, 8000, humanPost("what about lunch", ""))
	r.post(qid('h'), aliceID, 7000, humanPost("Q1 MIT", ""))
	if err := r.index.PlaceThread("txn-c", coordID, PlacedThread{N: 1, Close: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.index.PlaceThread("txn-r", coordID, PlacedThread{ReplyTo: qid('u')}); err != nil {
		t.Fatal(err)
	}
	if err := r.index.PlaceThread("txn-n", coordID, PlacedThread{N: 5}); err == nil {
		t.Error("a thread post placed on a question that is not indexed")
	}
	q := r.question(1)
	if q.State() != QuestionClosed || q.UnansweredCount() != 0 || len(r.index.Unattached()) != 0 {
		t.Errorf("before the echoes: %+v unattached %v", q, r.index.Unattached())
	}
	if st := r.index.State(); st.Questions[0].Closed || len(st.Questions[0].Unanswered) != 1 || len(st.Unattached) != 1 {
		t.Errorf("a provisional post reached the persisted state: %+v", st)
	}
	r.post(qid('c'), coordID, 6000, threadPost(1, qid('a'), qid('a'), true))
	r.post(qid('r'), coordID, 5000, plainReply(qid('u')))
	if st := r.index.State(); !st.Questions[0].Closed || len(st.Unattached) != 0 {
		t.Errorf("after the echoes: %+v", st)
	}
	if got := r.index.TakeDropped(); len(got) != 0 {
		t.Errorf("dropped = %v", got)
	}
}

func TestQuestionIndexCopyIsIndependent(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	n := ix.Next()
	if err := ix.PlaceQuestion("txn", coordID, Question{N: n}, questionBody(n)); err != nil {
		t.Fatal(err)
	}
	cp := ix.Copy()
	before := stateJSON(t, ix)
	askAgent(cp, 2)
	cp.Attach(humanEvent(eid(300), aliceID, 1), Human{Body: "Q1 hi"})
	if stateJSON(t, ix) != before {
		t.Error("folding the copy changed the original")
	}
	if q := mustQ(t, cp, 2); q.Provisional || q.EventID != eid(2) {
		t.Errorf("the copy lost the provisional entry it replaces: %+v", q)
	}
}

func TestQuestionIndexAnEchoFoldedAgainRemovesNoOtherEntry(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	for _, txn := range []string{"txn-1", "txn-2"} {
		if err := r.index.PlaceThread(txn, coordID, PlacedThread{N: 1}); err != nil {
			t.Fatal(err)
		}
	}
	first := r.judged(qid('c'), coordID, 8000, threadPost(1, qid('a'), qid('a'), false))
	r.fold(first)
	r.post(qid('h'), aliceID, 7000, humanPost("Q1 MIT", ""))
	r.fold(first)
	if got := r.index.TakeDropped(); len(got) != 0 {
		t.Errorf("a post folded again was reported dropped: %v", got)
	}
	if q := r.question(1); q.UnansweredCount() != 0 {
		t.Errorf("the second own post is no longer placed: %+v", q)
	}
	r.post(qid('d'), coordID, 6000, threadPost(1, qid('a'), qid('a'), false))
	if q := r.question(1); q.UnansweredCount() != 0 || q.Latest != qid('d') {
		t.Errorf("after the second echo: %+v", q)
	}
}
