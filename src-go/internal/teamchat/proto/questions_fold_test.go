package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// eid is the n-th distinct, well-formed event ID.
func eid(n int) string { return fmt.Sprintf("$%043d", n) }

func stateJSON(t *testing.T, x *QuestionIndex) string {
	t.Helper()
	data, err := json.Marshal(x.State())
	if err != nil {
		t.Fatalf("encoding the state: %v", err)
	}
	return string(data)
}

func questionBody(n int64) string { return QuestionName(int(n)) + " " + goodQuestion }

// mustQ is question n of the index, which the test says is there.
func mustQ(t *testing.T, x *QuestionIndex, n int64) IndexedQuestion {
	t.Helper()
	q, ok := x.Question(n)
	if !ok {
		t.Fatalf("Q%d is not indexed", n)
	}
	return q
}

// askVerdict is a counted-question verdict the way the judge makes it, without the judge.
func askVerdict(id, sender string, ts int64, q Question, body string) Verdict {
	return Verdict{
		Outcome: Index,
		Event:   Event{ID: id, Type: MessageType, Sender: sender, OriginTs: ts},
		Content: Content{Key: KeyQuestion, Body: body, Question: q},
	}
}

func threadVerdict(id, sender, root string, n int64, closes bool) Verdict {
	return Verdict{
		Outcome: Index,
		Event:   Event{ID: id, Type: MessageType, Sender: sender, OriginTs: 1},
		Content: Content{Key: KeyThread, Thread: Thread{N: n, Close: closes}, Relation: InThread(root, root, true)},
	}
}

func humanEvent(id, sender string, ts int64) Event {
	return Event{ID: id, Type: MessageType, Sender: sender, OriginTs: ts}
}

// askAgent counts agent question n as event eid(n).
func askAgent(ix *QuestionIndex, n int64) bool {
	return ix.Feed(askVerdict(eid(int(n)), coordID, 1000+n, Question{N: n}, questionBody(n)))
}

func plainReply(to string) string {
	return `{"msgtype":"m.notice","body":"which?","m.relates_to":` + replyRel(to) + `,"teamchat.thread":{"v":1}}`
}

func TestQuestionIndexIsIdempotentByEventID(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1 := qid('a')
	steps := []Verdict{
		r.judged(q1, coordID, 9000, questionPost(1, goodQuestion, "")),
		r.judged(qid('h'), aliceID, 8000, humanPost("MIT", threadRel(q1))),
		r.judged(qid('t'), coordID, 7000, threadPost(1, q1, q1, false)),
		r.judged(qid('i'), aliceID, 6000, humanPost("and", threadRel(q1))),
	}
	for _, v := range steps {
		r.fold(v)
	}
	want := stateJSON(t, r.index)
	if r.index.Feed(steps[0]) || r.index.Feed(steps[2]) {
		t.Error("Feed said it changed the index for an event already folded")
	}
	for _, v := range steps {
		r.fold(v)
	}
	if got := stateJSON(t, r.index); got != want {
		t.Errorf("folding the same events again changed the index:\n%s\n%s", want, got)
	}
	q := r.question(1)
	if q.Attached != 2 || len(q.Unanswered) != 1 || q.Unanswered[0] != qid('i') {
		t.Errorf("Q1 after the second pass = %+v, want 2 attached and only the last unanswered", q)
	}
}

func TestQuestionIndexAttachesWhateverTheVerdict(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1 := qid('a')
	r.post(q1, coordID, 9000, questionPost(1, goodQuestion, ""))
	r.judge.Roster.Limits.RecvPerSenderMinute = 10
	for i := range 12 {
		v := r.judged(qid(byte('A'+i)), aliceID, 6000, humanPost("MIT", threadRel(q1)))
		if dropped := v.Reason == ReasonRate; dropped != (i >= 10) {
			t.Fatalf("post %d: verdict %v %q, want a rate drop from the 11th", i, v.Outcome, v.Reason)
		}
		if n := r.fold(v); n != 1 {
			t.Errorf("post %d (%s) attached to Q%d, want Q1", i, v.Reason, n)
		}
	}
	if q := r.question(1); q.Attached != 12 || q.State() != QuestionAnswered {
		t.Errorf("Q1 = %+v, want 12 attached", q)
	}
}

func TestQuestionIndexAttachmentOfChangesNothing(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1 := qid('a')
	r.post(q1, coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('h'), aliceID, 8000, humanPost("Q1 hello", ""))
	before := stateJSON(t, r.index)
	cases := []struct {
		h    Human
		want int64
	}{
		{Human{Body: "Q1 yes"}, 1},
		{Human{Body: "plain", ThreadRoot: q1}, 1},
		{Human{Body: "plain", ReplyTo: q1}, 1},
		{Human{Body: "Q7 no such"}, 0},
		{Human{Body: "plain"}, 0},
		{Human{Body: "* better", Revises: qid('h')}, 1},
	}
	for _, tc := range cases {
		if n := r.index.AttachmentOf(tc.h); n != tc.want {
			t.Errorf("AttachmentOf(%+v) = Q%d, want Q%d", tc.h, n, tc.want)
		}
	}
	if after := stateJSON(t, r.index); after != before {
		t.Errorf("AttachmentOf changed the index:\n%s\n%s", before, after)
	}
}

func TestQuestionIndexUnattachedPostsAndTheirReplies(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('u'), aliceID, 8000, humanPost("what about lunch", ""))
	r.post(qid('v'), aliceID, 7000, humanPost("or tea", ""))
	r.post(qid('w'), aliceID, 6000, humanPost("Q1 yes", ""))
	want := []UnattachedPost{{ID: qid('u'), From: aliceID, SentMs: testNow - 8000}, {ID: qid('v'), From: aliceID, SentMs: testNow - 7000}}
	if got := r.index.Unattached(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Unattached() = %+v, want %+v (the attached post is not one)", got, want)
	}
	r.post(qid('x'), coordID, 5000, plainReply(qid('w')))
	r.post(qid('y'), coordID, 5000, plainReply(qid('z')))
	if got := r.index.Unattached(); len(got) != 2 {
		t.Errorf("a reply to an attached post or to an unknown one changed the list: %+v", got)
	}
	reply := r.judged(qid('r'), coordID, 4000, plainReply(qid('u')))
	if !r.index.Feed(reply) {
		t.Error("the agent's plain reply to an unattached post changed nothing")
	}
	if got := r.index.Unattached(); len(got) != 1 || got[0].ID != qid('v') {
		t.Errorf("Unattached() after the reply = %+v, want only the unreplied post", got)
	}
	if r.index.Feed(reply) {
		t.Error("the reply folded twice")
	}
	if n, known := r.index.Attachment(qid('u')); !known || n != 0 {
		t.Errorf("Attachment(replied-to post) = %d %v, want 0 true: the index saw it unattached", n, known)
	}
}

func TestQuestionIndexKeepsTheQuestionsForm(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	q := Question{
		N: 1, RemindAfterS: 3600, Re: []string{eid(90), eid(91)},
		AskedBy: []string{workerID, "@somebody:elsewhere.example", aliceID, "not a user id"},
	}
	ix.Feed(askVerdict(eid(1), coordID, 5000, q, questionBody(1)))
	got := mustQ(t, ix, 1)
	if len(got.AskedBy) != 2 || got.AskedBy[0] != workerID || got.AskedBy[1] != aliceID {
		t.Errorf("asked_by = %q, want the two user ids of this server", got.AskedBy)
	}
	if len(got.Re) != 2 || got.Re[0] != eid(90) || got.AskedMs != 5000 || got.RemindAfterS != 3600 {
		t.Errorf("question = %+v", got)
	}
	sum := sha256.Sum256([]byte("MIT or CC-BY?"))
	if got.Hash != hex.EncodeToString(sum[:]) {
		t.Errorf("hash = %q, want the SHA-256 of the collapsed Question: section", got.Hash)
	}
	if n, ok := ix.OpenDuplicate("  MIT\tor  CC-BY?\n"); !ok || n != 1 {
		t.Errorf("OpenDuplicate of the same section = %d %v", n, ok)
	}
	if _, ok := ix.OpenDuplicate("MIT or Apache?"); ok {
		t.Error("OpenDuplicate found another section")
	}
}

func TestQuestionIndexDropsAnAskedByTooLongToStore(t *testing.T) {
	server := strings.Repeat("s", 61) + "." + strings.Repeat("t", 61) + ".x"
	long := UserID("alice", server)
	if len(long) <= maxIndexedUserID {
		t.Fatalf("setup: %q is %d bytes", long, len(long))
	}
	ix := NewQuestionIndex(server, 0)
	ix.Feed(askVerdict(eid(1), UserID("myrepo.1+desk.podman", server), 1, Question{N: 1, AskedBy: []string{long}}, questionBody(1)))
	if q := mustQ(t, ix, 1); len(q.AskedBy) != 0 {
		t.Errorf("a user id over 128 bytes was kept: %+v", q)
	}
}

func TestQuestionIndexUnansweredIDs(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	for i := range 20 {
		ix.Attach(humanEvent(eid(100+i), aliceID, int64(i)), Human{Body: "Q1 more"})
	}
	q := mustQ(t, ix, 1)
	if len(q.Unanswered) != 16 || q.Unanswered[0] != eid(100) || q.Unanswered[15] != eid(115) || q.UnansweredMore != 4 {
		t.Errorf("unanswered = %v more %d, want the 16 oldest and 4 counted", q.Unanswered, q.UnansweredMore)
	}
	if q.UnansweredCount() != 20 || q.Attached != 20 {
		t.Errorf("counts = %d unanswered, %d attached", q.UnansweredCount(), q.Attached)
	}
	ix.Feed(threadVerdict(eid(200), coordID, eid(1), 1, false))
	if q = mustQ(t, ix, 1); len(q.Unanswered) != 0 || q.UnansweredMore != 0 || q.Attached != 20 || q.Latest != eid(200) {
		t.Errorf("after the agent's thread post: %+v", q)
	}
	ix.Attach(humanEvent(eid(201), aliceID, 1), Human{Body: "Q1 again"})
	if q = mustQ(t, ix, 1); len(q.Unanswered) != 1 || q.Unanswered[0] != eid(201) {
		t.Errorf("a post after the reset: %v", q.Unanswered)
	}
}

func TestQuestionIndexOpenCap(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	for n := int64(1); n <= MaxOpenQuestions; n++ {
		askAgent(ix, n)
	}
	if ix.Open() != 64 || ix.Evicted() != 0 {
		t.Fatalf("64 questions: open %d evicted %d", ix.Open(), ix.Evicted())
	}
	ix.Feed(askVerdict(eid(500), adminID, 5, Question{N: 100}, questionBody(100)))
	if _, ok := ix.Question(1); ok || ix.Open() != 64 || ix.Evicted() != 1 {
		t.Errorf("the admin's carried 65th: Q1 kept %v, open %d, evicted %d", ok, ix.Open(), ix.Evicted())
	}
	if _, ok := ix.Question(2); !ok {
		t.Error("the second oldest was evicted")
	}
	if ix.Next() != 101 {
		t.Errorf("Next() = %d, want 101: an evicted number is still below the highest", ix.Next())
	}
	ix.Feed(threadVerdict(eid(501), coordID, eid(2), 2, true))
	ix.Feed(askVerdict(eid(502), adminID, 6, Question{N: 101}, questionBody(101)))
	if ix.Open() != 64 || ix.Evicted() != 1 {
		t.Errorf("a closed question makes room: open %d, evicted %d", ix.Open(), ix.Evicted())
	}
}

func TestQuestionIndexClosedCap(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	for n := int64(1); n <= 130; n++ {
		askAgent(ix, n)
		ix.Feed(threadVerdict(eid(1000+int(n)), coordID, eid(int(n)), n, true))
	}
	st := ix.State()
	if len(st.Questions) != MaxClosedQuestions || ix.Open() != 0 {
		t.Fatalf("kept %d, open %d, want 128 closed", len(st.Questions), ix.Open())
	}
	for n, want := range map[int64]bool{1: false, 2: false, 3: true, 130: true} {
		if _, ok := ix.Question(n); ok != want {
			t.Errorf("Q%d kept = %v, want %v", n, ok, want)
		}
	}
}

func humanStream(ix *QuestionIndex, from, to int) {
	for i := from; i < to; i++ {
		ix.Attach(humanEvent(eid(10_000+i), aliceID, int64(i)), Human{Body: "Q1 more"})
	}
}

func TestQuestionIndexKeepsTheNewestAttachments(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	humanStream(ix, 0, 1100)
	if n := len(ix.State().Posts); n != MaxAttachments {
		t.Fatalf("%d attachments kept, want %d", n, MaxAttachments)
	}
	for i, want := range map[int]bool{0: false, 75: false, 76: true, 1099: true} {
		if _, known := ix.Attachment(eid(10_000 + i)); known != want {
			t.Errorf("post %d known = %v, want %v", i, known, want)
		}
	}
	if q := mustQ(t, ix, 1); q.Attached != 1100 || q.UnansweredCount() != 1100 || len(q.Unanswered) != 16 {
		t.Errorf("Q1 = %+v: the counts go on past the lists", q)
	}
}

func TestQuestionIndexKeepsTheNewestUnattachedPosts(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	for i := range 70 {
		ix.Attach(humanEvent(eid(2000+i), aliceID, int64(i)), Human{Body: "no question"})
	}
	got := ix.Unattached()
	if len(got) != MaxUnattached || got[0].ID != eid(2006) || got[63].ID != eid(2069) {
		t.Errorf("kept %d, want the newest 64 (2006 to 2069): %+v", len(got), got)
	}
}
