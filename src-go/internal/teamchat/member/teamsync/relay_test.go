package teamsync

import (
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// askedFor posts the admin's question numbered n asked for the seats given (a carried question
// keeps its `asked_by`, A 7.7) and gives its id.
func (w *world) askedFor(n int64, seats ...string) string {
	w.t.Helper()
	composer := proto.Composer{Server: server, Self: proto.AdminID(server), Roster: w.readRoster(), Questions: proto.NewQuestionIndex(server, 0)}
	out, err := composer.Question(proto.QuestionPost{
		N: n, AskedBy: seats,
		Text: "Which port does the deploy use?\nContext: the deploy moved hosts.\nQuestion: which port should it listen on?",
	})
	check(w.t, err)
	id, err := w.admin().Send(w.t.Context(), w.qroom(), proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return id
}

// relayTo posts the coordinator's relay to seat of the questions post id, attached to Q<q>.
func (w *world) relayTo(seat, id string, q int64) string {
	w.t.Helper()
	text := proto.Text{To: []string{seat}, Text: proto.RelayText(q), Relay: &proto.Relay{Room: proto.PurposeQuestions, ID: id, Q: q}}
	out, err := proto.Composer{Server: server, Self: user(coord), Token: w.tokens[coord], Roster: w.readRoster()}.Text(text, "", text.Text)
	check(w.t, err)
	sent, err := w.client(coord).Send(w.t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return sent
}

// A 10.6, the coordinator's side: a human post the sync folds onto Q4, asked for me, is owed to
// me as one relay, under the transaction ID derived from the post and the seat; a post on a
// question asked for nobody is relayed to nobody.
func TestTheCoordinatorRelaysAnAnswerToTheAskingSeat(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	w.readyCoordinator()
	q4 := w.askedFor(4, user(me))
	q5 := w.askedFor(5)
	w.batchWith(lastIs(q5))
	// The answer is later than the questions, which raised the room's floor (QS5).
	w.clk.Advance(time.Second)
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q4))
	b := w.batchWith(lastIs(answer))
	if len(b.Relays) != 1 || b.Relays[0].Txn != proto.RelayTxn(answer, user(me)) {
		t.Fatalf("relays %+v, want one to me", b.Relays)
	}
	v, err := proto.Parse(b.Relays[0].Content)
	check(t, err)
	c, reason := proto.DecodeContent(v, proto.MainKeys)
	if reason != "" || c.Text.Relay == nil || *c.Text.Relay != (proto.Relay{Room: proto.PurposeQuestions, ID: answer, Q: 4}) || !slices.Equal(c.Text.To, []string{user(me)}) {
		t.Fatalf("relay content %s (%q)", b.Relays[0].Content, reason)
	}
	other := w.sayIn(w.qroom(), alice, inThread("no idea", q5))
	if b := w.batchWith(lastIs(other)); len(b.Relays) != 0 {
		t.Errorf("a post on a question asked for nobody relayed: %+v", b.Relays)
	}
}

// A 10.6, the worker's side: a relay from the coordinator is delivered as the human's own post,
// fetched from questions by the worker's link (which joins the room to read it and never syncs
// it): a HUMAN item in `room: questions` with the relay's account. The relay was sent while the
// worker was not running; it gets it when it runs.
func TestAWorkerGetsTheRelayedPostAsHuman(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	first := w.startTeam(w.newTeam())
	w.batch()
	first.end()
	q4 := w.askedFor(4, user(me))
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q4))
	w.relayTo(user(me), answer, 4)
	w.startTeam(w.newTeam())
	b := w.batch()
	if len(b.Items) != 1 {
		t.Fatalf("items %+v", b.Items)
	}
	it := b.Items[0]
	if p := it.Pending; p.ID != answer || p.Room != proto.PurposeQuestions || p.Item != records.KindHuman || p.From != user(alice) {
		t.Errorf("pending %+v", p)
	}
	if v := it.Verdict; v.Human.Body != "port 8080" || v.Relayed == nil || *v.Relayed != (proto.Relayed{By: user(coord), Q: 4}) {
		t.Errorf("verdict %+v relayed %+v", v.Human, v.Relayed)
	}
}

// A 10.6: a relay naming a post that is not a team human's (the coordinator's own thread post) is
// dropped `provenance`, never shown as PEER.
func TestAForgedRelayIsDropped(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	_, release := w.held(w.newTeam())
	q4 := w.askedFor(4, user(me))
	w.relayTo(user(me), q4, 4)
	release()
	b := w.batch()
	if len(b.Items) != 0 || b.Counts.Dropped != 1 {
		t.Fatalf("items %+v counts %+v, want the relay dropped", b.Items, b.Counts)
	}
}

// B 2.7.3 and A 10.6: a relayed post restored after a restart is fetched again by the worker
// (which joins questions to read it) and judged a human's post fetched by ID: delivered, with no
// relay account, which the restart loses. A coordinator's post fetched so is gone.
func TestAWorkersRefetchOfARelayedPost(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	q4 := w.askedFor(4, user(me))
	answer := w.sayIn(w.qroom(), alice, inThread("port 8080", q4))
	tm := w.newTeam()
	v, err := tm.Refetch(t.Context(), proto.PurposeQuestions, answer)
	check(t, err)
	if v.Outcome != proto.Deliver || v.Kind != proto.KindHuman || v.Event.ID != answer || v.Relayed != nil {
		t.Errorf("verdict %v %q %+v, want the human's post delivered", v.Outcome, v.Reason, v.Relayed)
	}
	if v, err := tm.Refetch(t.Context(), proto.PurposeQuestions, q4); err != nil || v.Outcome != proto.Drop {
		t.Errorf("the admin's question refetched by a worker: %v %v", v.Outcome, err)
	}
}
