package teamsync

import (
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// QS5, A 10.6 (Plan 00209, project_floor_by_time): an owner's answer posted in the same
// millisecond as the question before it is not below the questions room's floor, which that
// question raised: it is delivered and relayed to the asking seat. The clock does not move
// between the posts, so every one shares a millisecond.
func TestAnAnswerInTheQuestionsMillisecondIsDeliveredAndRelayed(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	w.readyCoordinator()
	q4 := w.askedFor(4, user(me))
	w.batchWith(lastIs(q4))
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q4))
	if at, asked := w.qts(answer), w.qts(q4); at != asked {
		t.Fatalf("the answer at %d, the question at %d: the test needs one millisecond", at, asked)
	}
	b := w.batchWith(lastIs(answer))
	if !slices.Contains(itemIDs(b), answer) {
		t.Errorf("items %v: the answer of the question's millisecond was not delivered", itemIDs(b))
	}
	if len(b.Relays) != 1 || b.Relays[0].Txn != proto.RelayTxn(answer, user(me)) {
		t.Errorf("relays %+v, want the answer relayed to me", b.Relays)
	}
	second := w.sayIn(w.qroom(), alice, inThread("or 8443", q4))
	b = w.batchWith(lastIs(second))
	if !slices.Contains(itemIDs(b), second) || slices.Contains(itemIDs(b), answer) {
		t.Errorf("items %v: want the second answer of the millisecond, and the first not again", itemIDs(b))
	}
}

// qts is the origin time of the questions room's event id.
func (w *world) qts(id string) int64 {
	w.t.Helper()
	ev, err := w.admin().GetEvent(w.t.Context(), w.qroom(), id)
	check(w.t, err)
	v, err := proto.Parse(ev)
	check(w.t, err)
	return originTs(v)
}
