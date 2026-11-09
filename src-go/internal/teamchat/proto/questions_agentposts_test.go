package proto

import (
	"strings"
	"testing"
)

// The tests of SPEC-n3f QX6's second list (OW5 (b)): agent thread posts and held slots are
// remembered by event id in a list of their own, 256 entries, so the human posts keep all 1024.

// agentThreadStream folds agent thread posts in Q1's thread, eid(30_000+from) to eid(30_000+to-1).
func agentThreadStream(ix *QuestionIndex, from, to int) {
	for i := from; i < to; i++ {
		ix.Feed(threadVerdict(eid(30_000+i), coordID, eid(1), 1, false))
	}
}

func TestQuestionIndexKeepsTheNewestAgentPosts(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	agentThreadStream(ix, 0, 300)
	st := ix.State()
	if len(st.AgentPosts) != MaxAgentPosts || MaxAgentPosts != 256 {
		t.Fatalf("%d agent posts kept, want %d (256)", len(st.AgentPosts), MaxAgentPosts)
	}
	if st.AgentPosts[0] != (AgentPost{ID: eid(30_044), Q: 1}) || st.AgentPosts[255].ID != eid(30_299) {
		t.Errorf("kept %+v to %+v, want the newest 256, 30044 to 30299, oldest first", st.AgentPosts[0], st.AgentPosts[255])
	}
	if len(st.Posts) != 0 {
		t.Errorf("an agent thread post is in the human posts' list: %+v", st.Posts)
	}
	if ix.Feed(threadVerdict(eid(30_044), coordID, eid(1), 1, false)) {
		t.Error("the oldest agent post still kept was folded again")
	}
	if !ix.Feed(threadVerdict(eid(30_043), coordID, eid(1), 1, false)) {
		t.Error("the newest agent post evicted is still remembered")
	}
	if _, known := ix.Attachment(eid(30_299)); known {
		t.Error("an agent thread post still kept is known as a human post")
	}
}

// A copy holds both lists as they are, evicts past the caps on its own, and shares nothing.
func TestQuestionIndexCopyKeepsBothLists(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	humanStream(ix, 0, MaxAttachments+50)
	agentThreadStream(ix, 0, MaxAgentPosts+50)
	before := stateJSON(t, ix)
	c := ix.Copy()
	if got := stateJSON(t, c); got != before {
		t.Fatalf("the copy's state differs:\n%s\n%s", before, got)
	}
	agentThreadStream(c, 1000, 1002)
	c.Slot(eid(40_000))
	humanStream(c, 5000, 5002)
	st := c.State()
	if len(st.AgentPosts) != MaxAgentPosts || st.AgentPosts[0].ID != eid(30_053) || len(st.Posts) != MaxAttachments || st.Posts[0].ID != eid(10_052) {
		t.Errorf("the copy kept %d agent posts from %v and %d posts from %v, want 256 from 30053 and 1024 from 10052",
			len(st.AgentPosts), st.AgentPosts, len(st.Posts), st.Posts[0])
	}
	if got := stateJSON(t, ix); got != before {
		t.Error("folding into the copy changed the index it was copied from")
	}
}

// A 10.4 rules 2 and 4 read each list for what it holds: a reply attaches through a human post
// only when it is in a thread and through an agent post only while its question is kept, and an
// edit takes its original's attachment from a human post only.
func TestQuestionIndexRulesTwoAndFourReadEachList(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	ix.Attach(humanEvent(eid(10_000), aliceID, 1), Human{Body: "Q1 by name"})
	if _, ok := ix.Attach(humanEvent(eid(10_001), aliceID, 1), Human{Body: "re", ReplyTo: eid(10_000)}); ok {
		t.Error("a reply to a human post outside any thread attached by rule 2")
	}
	ix.Feed(threadVerdict(eid(30_000), coordID, eid(1), 1, true))
	if _, ok := ix.Attach(humanEvent(eid(10_002), aliceID, 1), Human{Body: "* fixed", Revises: eid(30_000)}); ok {
		t.Error("an edit of an agent thread post took the post's question by rule 4")
	}
	for n := int64(2); n <= MaxClosedQuestions+1; n++ {
		askAgent(ix, n)
		ix.Feed(threadVerdict(eid(31_000+int(n)), coordID, eid(int(n)), n, true))
	}
	if _, held := ix.Question(1); held {
		t.Fatal("Q1 is still kept past the closed cap")
	}
	if _, ok := ix.Attach(humanEvent(eid(10_003), aliceID, 1), Human{Body: "re", ReplyTo: eid(30_000)}); ok {
		t.Error("a reply to an agent thread post of a question no longer kept attached")
	}
	if n, known := ix.Attachment(eid(10_003)); !known || n != 0 {
		t.Errorf("the reply is remembered as attached to Q%d (known %v), want to none", n, known)
	}
}

func TestQuestionIndexSlotsShareTheAgentPostsList(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	for i := range MaxAgentPosts + 1 {
		ix.Slot(eid(40_000 + i))
	}
	st := ix.State()
	if len(st.AgentPosts) != MaxAgentPosts || st.Slots != 257 {
		t.Fatalf("slots: %d kept, %d counted; want 256 and 257", len(st.AgentPosts), st.Slots)
	}
	if st.AgentPosts[0] != (AgentPost{ID: eid(40_001)}) {
		t.Errorf("the oldest slot kept is %+v, want 40001 with no question", st.AgentPosts[0])
	}
	if ix.Slot(eid(40_001)) {
		t.Error("the oldest slot still kept was folded again")
	}
	if !ix.Slot(eid(40_000)) || ix.State().Slots != 258 {
		t.Errorf("the evicted slot is still remembered: slots %d", ix.State().Slots)
	}
}

// Agent thread posts and slots, however many, push no human post out of its list.
func TestQuestionIndexAgentPostsLeaveTheHumanPostsAlone(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	humanStream(ix, 0, MaxAttachments)
	agentThreadStream(ix, 0, 2*MaxAgentPosts)
	for i := range 2 * MaxAgentPosts {
		ix.Slot(eid(40_000 + i))
	}
	for _, i := range []int{0, 1, MaxAttachments - 1} {
		if n, known := ix.Attachment(eid(10_000 + i)); !known || n != 1 {
			t.Errorf("human post %d: Q%d known %v, want Q1 known", i, n, known)
		}
	}
	if st := ix.State(); len(st.Posts) != MaxAttachments || len(st.AgentPosts) != MaxAgentPosts {
		t.Errorf("%d human posts and %d agent posts kept, want %d and %d", len(st.Posts), len(st.AgentPosts), MaxAttachments, MaxAgentPosts)
	}
	if _, known := ix.Attachment(eid(30_500)); known {
		t.Error("an agent thread post is known as a human post")
	}
}

// A 10.4: a thread post fed again after later human posts does not reset their unanswered count,
// however many human posts came between, and a human reply to an agent thread post attaches.
func TestQuestionIndexAThreadPostFedAgainResetsNothing(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	thread := threadVerdict(eid(30_000), coordID, eid(1), 1, false)
	ix.Feed(thread)
	humanStream(ix, 0, MaxAttachments+100)
	if ix.Feed(thread) {
		t.Error("the thread post fed again counted")
	}
	if q := mustQ(t, ix, 1); q.UnansweredCount() != MaxAttachments+100 {
		t.Errorf("Q1's unanswered count is %d after the thread post was fed again, want %d", q.UnansweredCount(), MaxAttachments+100)
	}
	q, ok := ix.Attach(humanEvent(eid(50_000), aliceID, 1), Human{Body: "re", ReplyTo: eid(30_000)})
	if !ok || q.N != 1 {
		t.Errorf("a reply to an agent thread post attached to Q%d (%v), want Q1 (rule 2)", q.N, ok)
	}
	if _, ok := ix.Attach(humanEvent(eid(50_001), aliceID, 1), Human{Body: "re", ReplyTo: eid(40_000)}); ok {
		t.Error("a reply to an unknown post attached")
	}
}

// An event id is folded as one kind only: a slot fed as a human post, or a human post fed as a
// slot or a thread post, changes nothing.
func TestQuestionIndexAnEventIDIsFoldedOnce(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	ix.Slot(eid(40_000))
	ix.Attach(humanEvent(eid(10_000), aliceID, 1), Human{Body: "Q1 yes"})
	ix.Feed(threadVerdict(eid(30_000), coordID, eid(1), 1, false))
	want := stateJSON(t, ix)
	if _, ok := ix.Attach(humanEvent(eid(40_000), aliceID, 1), Human{Body: "Q1 yes"}); ok {
		t.Error("a slot fed as a human post attached")
	}
	if q, ok := ix.Attach(humanEvent(eid(30_000), aliceID, 1), Human{Body: "Q1 yes"}); !ok || q.N != 1 || q.Attached != 1 {
		t.Errorf("a thread post fed as a human post: %+v (%v), want Q1 unchanged", q, ok)
	}
	if ix.Slot(eid(10_000)) || ix.Slot(eid(30_000)) {
		t.Error("a human post or a thread post fed as a slot counted")
	}
	if ix.Feed(threadVerdict(eid(10_000), coordID, eid(1), 1, false)) || ix.Feed(threadVerdict(eid(40_000), coordID, eid(1), 1, false)) {
		t.Error("a human post or a slot fed as a thread post counted")
	}
	if got := stateJSON(t, ix); got != want {
		t.Errorf("an event fed as another kind changed the index:\n%s\n%s", want, got)
	}
}

// QX6's sum: 192 questions of 5120, 1024 attachments and 256 agent posts of 96, 64 unattached
// posts of 256, each with its comma, and the fixed fields' 2048.
func TestQuestionsStateCapIsTheSum(t *testing.T) {
	sum := 192*5121 + MaxAttachments*97 + MaxAgentPosts*97 + MaxUnattached*257 + 2048
	if sum != 1_125_888 {
		t.Errorf("QX6's sum is %d, the spec states 1 125 888", sum)
	}
	if MaxQuestionsStateBytes != sum {
		t.Errorf("MaxQuestionsStateBytes = %d, QX6's sum is %d", MaxQuestionsStateBytes, sum)
	}
}

func TestQuestionsStateWritesTheTwoListsInTheirForms(t *testing.T) {
	doc := string(encodeOK(t, codecState(t)))
	for _, want := range []string{
		`"posts":[{"id":"` + eid(11) + `","q":1,"t":1},{"id":"` + eid(12) + `","q":0,"t":0}]`,
		`"agent_posts":[{"id":"` + eid(21) + `","q":2},{"id":"` + eid(31) + `","q":0}]`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the state does not hold %s:\n%s", want, doc)
		}
	}
}
