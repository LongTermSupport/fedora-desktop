package proto

import "testing"

// attachedTo is the question number an Attach result names, 0 for none.
func attachedTo(q IndexedQuestion, attached bool) int64 {
	if !attached {
		return 0
	}
	return q.N
}

func TestQuestionIndexSlotWithNoQuestionAfterIt(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	if !ix.Slot(eid(802)) || ix.Next() != 3 {
		t.Fatalf("Next() = %d, want 3", ix.Next())
	}
	if ix.Slot(eid(802)) || ix.Next() != 3 {
		t.Errorf("the same slot twice: Next() = %d", ix.Next())
	}
	if _, ok := ix.Question(2); ok {
		t.Error("a number nothing counted skipped has an entry")
	}
}

func TestQuestionIndexSlotHeldByTheNextQuestion(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	ix.Slot(eid(802))
	if !askAgent(ix, 3) || ix.Next() != 4 {
		t.Fatalf("Q3 counted false, or Next() = %d, want 4", ix.Next())
	}
	held := mustQ(t, ix, 2)
	if !held.Redacted() || held.State() != QuestionClosed || held.Headline != "" || held.Hash != "" {
		t.Errorf("Q2 = %+v, want a held, closed, redacted entry", held)
	}
	if got := attachedTo(ix.Attach(humanEvent(eid(900), aliceID, 1), Human{Body: "Q2: a late answer"})); got != 0 {
		t.Errorf("a late answer to the redacted Q2 attached to Q%d", got)
	}
	if q := mustQ(t, ix, 2); q.Attached != 0 {
		t.Errorf("the held entry took an attachment: %+v", q)
	}
}

func TestQuestionIndexSlotWidensTheCountingWindow(t *testing.T) {
	for _, n := range []int64{2, 3} {
		ix := NewQuestionIndex(testServer, 0)
		askAgent(ix, 1)
		ix.Slot(eid(802))
		if !askAgent(ix, n) || ix.Next() != n+1 {
			t.Errorf("Q%d inside the window of Q2 to Q3: Next() = %d, want %d and the count reset", n, ix.Next(), n+1)
		}
	}
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	ix.Slot(eid(802))
	if askAgent(ix, 4) {
		t.Error("Q4 counted past Next() 3")
	}
}

func TestQuestionIndexSlotBeforeAnAdminQuestionBelowTheHighest(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	ix.Feed(askVerdict(eid(9), adminID, 1, Question{N: 9}, questionBody(9)))
	ix.Slot(eid(802))
	ix.Feed(askVerdict(eid(3), adminID, 1, Question{N: 3}, questionBody(3)))
	if _, ok := ix.Question(3); !ok || ix.Next() != 11 {
		t.Errorf("Q3 counted %v, Next() = %d, want 11", ok, ix.Next())
	}
}

func TestQuestionIndexSlotBeforeAnAdminJump(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	ix.Slot(eid(802))
	ix.Slot(eid(803))
	ix.Feed(askVerdict(eid(9), adminID, 1, Question{N: 9}, questionBody(9)))
	for n, want := range map[int64]bool{2: true, 3: true, 4: false, 8: false, 9: true} {
		if _, ok := ix.Question(n); ok != want {
			t.Errorf("Q%d present = %v, want %v", n, ok, want)
		}
	}
	if ix.Next() != 10 {
		t.Errorf("Next() = %d", ix.Next())
	}
}

func TestQuestionIndexSlotOfAFoldedEvent(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	if ix.Slot(eid(1)) || ix.Next() != 2 {
		t.Errorf("a counted question's id as a slot: Next() = %d", ix.Next())
	}
}
