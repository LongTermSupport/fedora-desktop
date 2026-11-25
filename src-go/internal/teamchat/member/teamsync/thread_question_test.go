package teamsync

import (
	"testing"
)

// R34 (Plan 00265): a human's reply inside the thread of the coordinator's own question is
// attached to that question by the index the sync folds, as verify names it.
func TestR34AHumanReplyInTheOwnQuestionsThreadIsAttached(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	rt := w.readyWith(w.main, nil)
	asks(t, rt.tm, "tcmine")
	echo := w.sayIn(w.qroom(), w.self, rawContent(t, w.ownQuestion()))
	rt.r.settled(t, w.batchWith(lastIs(echo)))
	answer := w.sayIn(w.qroom(), alice, inThread("MIT", echo))
	b := w.batchWith(lastIs(answer))
	if n, seen := keyIndex(t, b).Attachment(answer); !seen || n != 1 {
		t.Errorf("the in-thread reply is attached to %d (seen %v), want Q1", n, seen)
	}
}
