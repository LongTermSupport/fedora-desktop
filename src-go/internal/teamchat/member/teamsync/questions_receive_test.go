package teamsync

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

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
	w.readyWith(w.main, nil)
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

// adminQuestion posts the admin's question numbered n in questions (A 6.5's exception: the
// admin's question counts whatever its number) and gives its id.
func (w *world) adminQuestion(n int64) string {
	w.t.Helper()
	composer := proto.Composer{Server: server, Self: proto.AdminID(server), Roster: w.readRoster(), Questions: proto.NewQuestionIndex(server, 0)}
	out, err := composer.Question(proto.QuestionPost{
		N: n, Text: "Which port does the deploy use?\nContext: the deploy moved hosts.\nQuestion: which port should it listen on?",
	})
	check(w.t, err)
	id, err := w.admin().Send(w.t.Context(), w.qroom(), proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return id
}

// edit is a human's edit of id with the new body.
func edit(body, id string) map[string]any {
	return map[string]any{
		"msgtype": "m.text", "body": "* " + body,
		"m.new_content": map[string]any{"msgtype": "m.text", "body": body},
		"m.relates_to":  map[string]any{"rel_type": "m.replace", "event_id": id},
	}
}

// QS6 (A 10.4): with a question in the index, a human post in its thread, a reply to a thread
// post, a `Q3:` prefix and an edit of an attached post are each delivered in `room:
// questions` and attached to the question; the edit is an item with `revises` (A 7.5).
func TestQS6PostsAttachToTheQuestionByEachRule(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	w.readyCoordinator()
	q := w.adminQuestion(3)
	b := w.batchWith(lastIs(q))
	if _, ok := keyIndex(t, b).Question(3); !ok {
		t.Fatal("the admin's Q3 is not in the index: the posts below judge nothing")
	}
	var thread string
	for _, c := range []struct {
		name    string
		content func() map[string]any
		revises bool
	}{
		{"in the thread", func() map[string]any { return inThread("port 8080, I think", q) }, false},
		{"a reply to a thread post", func() map[string]any { return reply("or 8443", thread) }, false},
		{"a Q3: prefix", func() map[string]any { return human("Q3: it is 8080") }, false},
		{"an edit of an attached post", func() map[string]any { return edit("port 9090 after all", thread) }, true},
	} {
		w.clk.StepWall(1000)
		id := w.sayIn(w.qroom(), alice, c.content())
		if thread == "" {
			thread = id
		}
		b := w.batchWith(lastIs(id))
		var item *team.Item
		for i := range b.Items {
			if b.Items[i].Pending.ID == id && b.Items[i].Pending.Room == proto.PurposeQuestions {
				item = &b.Items[i]
			}
		}
		if item == nil {
			t.Errorf("%s: not delivered in questions (items %v)", c.name, itemIDs(b))
			continue
		}
		if c.revises && item.Verdict.Human.Revises != thread {
			t.Errorf("%s: revises %q, want %q", c.name, item.Verdict.Human.Revises, thread)
		}
		if n, seen := keyIndex(t, b).Attachment(id); !seen || n != 3 {
			t.Errorf("%s: attached to %d (seen %v), want Q3", c.name, n, seen)
		}
	}
}

// QS6: a human post in questions is never judged `stale`: one older than human_max_age_s is
// delivered there, while one as old in main is not.
func TestQS6AnOldHumanPostIsStillDelivered(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	rt := w.readyWith(w.main, func(tm *Team) { g = gateFilter(t, tm, w.filterFor(true)) })
	release := g.armed()
	tick := w.sayIn(w.qroom(), alice, human("a first answer"))
	w.batchWith(lastIs(tick))
	g.isHeld()
	w.clk.StepWall(1000)
	old := w.sayIn(w.qroom(), alice, human("an old post"))
	inMain := w.say(alice, human("as old, in main", user(coord)))
	w.clk.StepWall((w.roster.Limits.HumanMaxAgeS + 1) * 1000)
	rt.drainPolls()
	release()
	b := w.persistedBefore(rt.r)
	got := itemIDs(b)
	if !slices.Contains(got, old) {
		t.Errorf("items %v: the old questions post was not delivered", got)
	}
	if slices.Contains(got, inMain) {
		t.Errorf("items %v: main's post as old was delivered, so the test judged no age", got)
	}
}

// QS6 (n3f-D18): a human post dropped `rate` (past recv_per_sender_minute) is still attached.
func TestQS6APostDroppedRateIsStillAttached(t *testing.T) {
	w := newWorld(t)
	w.inQuestions(alice, false)
	var g *filterGate
	rt := w.readyWith(w.main, func(tm *Team) { g = gateFilter(t, tm, w.filterFor(true)) })
	release := g.armed()
	tick := w.sayIn(w.qroom(), alice, human("a first answer"))
	w.batchWith(lastIs(tick))
	g.isHeld()
	// Past recv_per_sender_minute in one answer (and under its timeline limit of 100).
	n := int(w.roster.Limits.RecvPerSenderMinute) + 3
	ids := make([]string, 0, n)
	for i := range n {
		// Each later than the floor the first answer left (its post's millisecond).
		w.clk.StepWall(1)
		ids = append(ids, w.sayIn(w.qroom(), alice, human(fmt.Sprintf("post %d", i))))
	}
	rt.drainPolls()
	release()
	b := w.persistedBefore(rt.r)
	if b.Counts.Dropped == 0 {
		t.Fatalf("counts %+v: no post was dropped `rate`, so the test judges nothing", b.Counts)
	}
	delivered := itemIDs(b)
	idx := keyIndex(t, b)
	for _, id := range ids {
		if _, seen := idx.Attachment(id); !seen {
			t.Errorf("the post %s (delivered %v) is not attached", id, slices.Contains(delivered, id))
		}
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
