package teamsync

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// inQuestions joins local to the questions room (the admin's invite first when it has none) so
// that it can post there.
func (w *world) inQuestions(local string, invite bool) {
	w.t.Helper()
	if invite {
		w.invited(local)
	}
	if _, err := w.client(local).Join(w.t.Context(), w.qroom()); err != nil {
		w.t.Fatalf("%s joins questions: %v", local, err)
	}
}

// sayIn posts content as local in room.
func (w *world) sayIn(room, local string, content map[string]any) string {
	w.t.Helper()
	data, err := json.Marshal(content)
	check(w.t, err)
	id, err := w.client(local).Send(w.t.Context(), room, proto.MessageType, "", data)
	check(w.t, err)
	return id
}

// readyCoordinator is the coordinator's team with its index at the head, at its first long-poll
// with the coordinator filter.
func (w *world) readyCoordinator() {
	w.t.Helper()
	w.asCoordinator()
	tm := w.newTeam(withBuilder(&fakeBuilder{}))
	filters := filtersOf(tm)
	r := w.startTeam(tm)
	w.batch()
	r.poll(w.t)
	tm.IndexAtHead(proto.NewQuestionIndex(server, 0), w.main)
	for coordinator := w.filterFor(true); nextFilter(w.t, filters) != coordinator; {
		continue
	}
	// What is posted next is later than the join, the room's floor (QS5), on the fake clock.
	w.clk.Advance(time.Second)
}

// lastIs is a batch whose questions key's last folded event is id.
func lastIs(id string) func(team.Batch) bool {
	return func(b team.Batch) bool {
		return b.Questions != nil && b.Questions.Last != nil && *b.Questions.Last == id
	}
}

// batchWith is the first of the next four batches that pred holds for: the room entering the
// filter has an answer of its own before the post's (its timeline up to then).
func (w *world) batchWith(pred func(team.Batch) bool) team.Batch {
	w.t.Helper()
	for range 4 {
		if b := w.batch(); pred(b) {
			return b
		}
	}
	w.t.Fatal("no batch of the kind within four")
	return team.Batch{}
}

func questionsItems(b team.Batch) []string {
	var ids []string
	for _, it := range b.Items {
		if it.Pending.Room == proto.PurposeQuestions {
			ids = append(ids, it.Pending.ID)
		}
	}
	return ids
}

// QS6: a human's post in questions needs no mention: it is delivered, its pending entry has
// `room: questions`, the index attaches it (here to no question), and the answer's write holds
// the `questions` key at the answer's position, its last event the post (QX5).
func TestQS6AnUnmentionedHumanPostIsDeliveredAndAttached(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	w.readyCoordinator()
	id := w.sayIn(w.qroom(), alice, map[string]any{"msgtype": "m.text", "body": "is the build green?"})
	b := w.batchWith(lastIs(id))
	if got := questionsItems(b); !slices.Equal(got, []string{id}) {
		t.Fatalf("questions items %v, want [%s]", got, id)
	}
	k := b.Questions
	if k == nil || k.Index == nil || k.Token == nil || k.Last == nil {
		t.Fatalf("the questions key %+v: the answer's write must hold it", k)
	}
	if *k.Token != b.NextBatch || *k.Last != id {
		t.Errorf("key token %q last %q, want %q and %q", *k.Token, *k.Last, b.NextBatch, id)
	}
	idx, err := proto.LoadQuestionIndex(server, 0, *k.Index)
	check(t, err)
	if u := idx.Unattached(); len(u) != 1 || u[0].ID != id {
		t.Errorf("unattached %+v, want the post", u)
	}
}

// QS6: another agent's post in questions is dropped `sender`: nothing is delivered.
func TestQS6AWorkersPostInQuestionsIsDropped(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(peer, true)
	w.readyCoordinator()
	id := w.sayIn(w.qroom(), peer, map[string]any{"msgtype": "m.text", "body": "not mine to post"})
	b := w.batchWith(lastIs(id))
	if got := questionsItems(b); len(got) != 0 {
		t.Errorf("a worker's post delivered: %v", got)
	}
	if b.Counts.Dropped == 0 {
		t.Errorf("counts %+v: the post was not dropped", b.Counts)
	}
}

// QS6: a main answer whose filter did not hold the questions room leaves the key as it was.
func TestQS6AnAnswerWithoutTheRoomLeavesTheKey(t *testing.T) {
	w := newWorld(t)
	w.asCoordinator()
	r := w.startTeam(w.newTeam(withBuilder(&fakeBuilder{})))
	w.batch()
	r.poll(t)
	w.agentText(peer, "status?", user(coord))
	if b := w.batch(); b.Questions != nil {
		t.Errorf("the key %+v written by an answer that did not hold the room", b.Questions)
	}
}
