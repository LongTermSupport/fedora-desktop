package proto

import (
	"strconv"
	"strings"
	"testing"
)

func qid(c byte) string { return "$" + strings.Repeat(string(c), 43) }

func jsonString(s string) string { return string(Str(s).Compact()) }

func roomEvent(id, sender string, ts int64, content string) string {
	return `{"type":"m.room.message","event_id":"` + id + `","sender":"` + sender + `","origin_server_ts":` +
		strconv.FormatInt(ts, 10) + `,"content":` + content + `}`
}

func questionPost(n int64, text, extra string) string {
	name := QuestionName(int(n))
	return `{"msgtype":"m.text","body":` + jsonString(name+" "+text) + `,"teamchat.question":{"v":1,"n":` +
		strconv.FormatInt(n, 10) + extra + `}}`
}

func threadPost(n int64, root, reply string, closes bool) string {
	rel := `{"rel_type":"m.thread","event_id":"` + root + `","is_falling_back":` + strconv.FormatBool(reply == root) +
		`,"m.in_reply_to":{"event_id":"` + reply + `"}}`
	return `{"msgtype":"m.notice","body":"follow-up","m.relates_to":` + rel + `,"teamchat.thread":{"v":1,"n":` +
		strconv.FormatInt(n, 10) + `,"close":` + strconv.FormatBool(closes) + `}}`
}

func humanPost(body, rel string) string {
	s := `{"msgtype":"m.text","body":` + jsonString(body)
	if rel != "" {
		s += `,"m.relates_to":` + rel
	}
	return s + `}`
}

func threadRel(root string) string {
	return `{"rel_type":"m.thread","event_id":"` + root + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` + root + `"}}`
}

func replyRel(id string) string { return `{"m.in_reply_to":{"event_id":"` + id + `"}}` }

// questionRoom feeds the coordinator's question index from events judged in questions.
type questionRoom struct {
	t      *testing.T
	judge  Judge
	index  *QuestionIndex
	lookup fakeLookup
}

func newQuestionRoom(t *testing.T, floor int64) *questionRoom {
	t.Helper()
	j := judge(t, coordID, PurposeQuestions)
	lookup := testLookup(coordID)
	j.Lookup = lookup
	return &questionRoom{t: t, judge: j, index: NewQuestionIndex(testServer, floor), lookup: lookup}
}

// post judges one event at testNow-ageMs and feeds the index; for a human post it gives the
// question the post is attached to, 0 for none.
func (r *questionRoom) post(id, sender string, ageMs int64, content string) int64 {
	r.t.Helper()
	v := r.judge.Judge(mustParse(r.t, roomEvent(id, sender, testNow-ageMs, content)))
	r.lookup.events[id] = Related{Sender: sender, RoomID: questionsRoom}
	if v.Outcome == Index {
		r.index.Feed(v)
		return 0
	}
	if v.Outcome != Deliver {
		r.t.Fatalf("event %s: verdict %v %q", id, v.Outcome, v.Reason)
	}
	q, attached := r.index.Attach(v)
	if !attached {
		return 0
	}
	return q.N
}

func (r *questionRoom) question(n int64) IndexedQuestion {
	r.t.Helper()
	q, ok := r.index.Question(n)
	if !ok {
		r.t.Fatalf("Q%d is not indexed", n)
	}
	return q
}

func TestQuestionIndexNumbering(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('b'), coordID, 8000, questionPost(3, goodQuestion, ""))
	r.post(qid('c'), workerID, 7000, questionPost(2, goodQuestion, ""))
	r.post(qid('d'), adminID, 6000, questionPost(7, goodQuestion, ""))
	r.post(qid('e'), coordID, 5000, questionPost(5, goodQuestion, ""))
	r.post(qid('f'), adminID, 4000, questionPost(2, goodQuestion, ""))
	r.post(qid('g'), coordID, 3000, questionPost(8, "no form", ""))
	for n, want := range map[int64]string{1: qid('a'), 2: qid('c'), 7: qid('d')} {
		if q := r.question(n); q.EventID != want || q.Headline != "Which licence for the docs site?" {
			t.Errorf("Q%d = %+v, want event %s", n, q, want)
		}
	}
	for _, n := range []int64{3, 5, 8} {
		if _, ok := r.index.Question(n); ok {
			t.Errorf("Q%d is counted", n)
		}
	}
	if r.index.Next() != 8 {
		t.Errorf("Next() = %d, want 8", r.index.Next())
	}
}

func TestQuestionIndexFloor(t *testing.T) {
	r := newQuestionRoom(t, 10)
	if r.index.Next() != 11 {
		t.Errorf("Next() at floor 10 = %d", r.index.Next())
	}
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('b'), coordID, 8000, questionPost(11, goodQuestion, ""))
	r.post(qid('c'), adminID, 7000, questionPost(4, goodQuestion, ""))
	if _, ok := r.index.Question(1); ok || r.question(11).EventID != qid('b') || r.question(4).EventID != qid('c') {
		t.Errorf("floor 10: Q1 counted %v", ok)
	}
	if r.index.Next() != 12 {
		t.Errorf("Next() = %d, want 12", r.index.Next())
	}
}

func TestQuestionIndexFeedsOnlyIndexVerdicts(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	if ix.Feed(Verdict{Outcome: Deliver}) {
		t.Error("Feed took a delivery")
	}
	if _, ok := ix.Attach(Verdict{Outcome: Index}); ok {
		t.Error("Attach took an index verdict")
	}
	if _, known := ix.Attachment(qid('z')); known {
		t.Error("an unknown post has an attachment")
	}
}

func TestQuestionIndexThreadPosts(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1, q2 := qid('a'), qid('b')
	r.post(q1, coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(q2, coordID, 8000, questionPost(2, goodQuestion, ""))
	if r.question(1).Latest != q1 {
		t.Errorf("an empty thread's latest is %s", r.question(1).Latest)
	}
	r.post(qid('c'), coordID, 7000, threadPost(1, q1, q1, false))
	r.post(qid('d'), coordID, 6000, threadPost(1, q2, q2, true))
	r.post(qid('e'), coordID, 5000, threadPost(9, q1, q1, true))
	r.post(qid('f'), coordID, 4000, `{"msgtype":"m.notice","body":"which?","m.relates_to":`+replyRel(q1)+`,"teamchat.thread":{"v":1}}`)
	if q := r.question(1); q.Latest != qid('c') || q.State() != QuestionOpen {
		t.Errorf("Q1 = %+v", q)
	}
	if q := r.question(2); q.Latest != q2 || q.State() != QuestionOpen {
		t.Errorf("a post rooted at another question changed Q2: %+v", q)
	}
	r.post(qid('g'), workerID, 3000, threadPost(2, q2, q2, true))
	if q := r.question(2); q.State() != QuestionClosed || q.Latest != qid('g') {
		t.Errorf("Q2 after its closing post = %+v", q)
	}
}

func TestQuestionIndexAttachment(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1, q2 := qid('a'), qid('b')
	r.post(q1, coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(q2, coordID, 8000, questionPost(2, goodQuestion, ""))
	r.post(qid('c'), coordID, 7000, threadPost(2, q2, q2, false))
	cases := []struct {
		id      byte
		content string
		want    int64
	}{
		{'h', humanPost("MIT", threadRel(q1)), 1},
		{'i', humanPost("yes", replyRel(q2)), 2},
		{'r', humanPost("and", replyRel(qid('c'))), 2},
		{'k', humanPost("and more", replyRel(qid('h'))), 1},
		{'l', humanPost("q2: CC-BY", ""), 2},
		{'m', humanPost("Q1.MIT", ""), 1},
		{'n', humanPost("Q9: what?", ""), 0},
		{'o', humanPost("Q1x", ""), 0},
		{'p', humanPost("unrelated", threadRel(qid('z'))), 0},
	}
	for _, tc := range cases {
		if n := r.post(qid(tc.id), aliceID, 6000, tc.content); n != tc.want {
			t.Errorf("post %c attached to Q%d, want Q%d", tc.id, n, tc.want)
		}
		if n, known := r.index.Attachment(qid(tc.id)); !known || n != tc.want {
			t.Errorf("Attachment(%c) = %d %v", tc.id, n, known)
		}
	}
	if q := r.question(1); q.Latest != qid('h') || q.State() != QuestionAnswered || q.Attached != 3 || q.Unanswered != 3 {
		t.Errorf("Q1 = %+v", q)
	}
	if q := r.question(2); q.Attached != 3 || q.Unanswered != 3 {
		t.Errorf("Q2 = %+v", q)
	}
	r.post(qid('q'), coordID, 5000, threadPost(1, q1, qid('h'), false))
	if q := r.question(1); q.Unanswered != 0 || q.Attached != 3 || q.Latest != qid('q') {
		t.Errorf("Q1 after the coordinator's post = %+v", q)
	}
}

func TestQuestionIndexEdits(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('b'), coordID, 8000, questionPost(2, goodQuestion, ""))
	r.post(qid('h'), aliceID, 7000, humanPost("MIT", threadRel(qid('a'))))
	r.post(qid('i'), aliceID, 7000, humanPost("no idea", ""))
	edit := func(original, body string) string {
		return `{"msgtype":"m.text","body":"* x","m.new_content":{"msgtype":"m.text","body":` + jsonString(body) +
			`},"m.relates_to":{"rel_type":"m.replace","event_id":"` + original + `"}}`
	}
	cases := []struct {
		id      byte
		content string
		want    int64
	}{
		{'r', edit(qid('h'), "Q2 CC-BY after all"), 1},
		{'k', edit(qid('i'), "Q2 CC-BY"), 2},
		{'l', edit(qid('i'), "still no idea"), 0},
	}
	for _, tc := range cases {
		if n := r.post(qid(tc.id), aliceID, 6000, tc.content); n != tc.want {
			t.Errorf("edit %c attached to Q%d, want Q%d", tc.id, n, tc.want)
		}
	}
}

func TestQuestionIndexOpenDuplicate(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('b'), coordID, 8000, questionPost(2, "Head\nContext: c\nQuestion: Ship  it\n now?", ""))
	r.post(qid('c'), coordID, 7000, questionPost(3, "Head\nContext: c\nQuestion: Merge?", ""))
	r.post(qid('h'), aliceID, 6000, humanPost("yes", threadRel(qid('b'))))
	r.post(qid('d'), coordID, 5000, threadPost(3, qid('c'), qid('c'), true))
	cases := map[string]int64{"MIT or CC-BY?": 1, "Ship it now?": 2, " Ship it\tnow? ": 2, "Merge?": 0, "Other?": 0}
	for question, want := range cases {
		n, ok := r.index.OpenDuplicate(question)
		if ok != (want != 0) || n != want {
			t.Errorf("OpenDuplicate(%q) = %d %v, want %d", question, n, ok, want)
		}
	}
}

func TestQuestionIndexReminders(t *testing.T) {
	r := newQuestionRoom(t, 0)
	hour := `,"remind_after_s":3600`
	r.post(qid('a'), coordID, 3_600_000+timeoutGraceMs+1, questionPost(1, goodQuestion, hour))
	r.post(qid('b'), coordID, 3_600_000+timeoutGraceMs, questionPost(2, goodQuestion, hour))
	r.post(qid('c'), coordID, 7_200_000, questionPost(3, goodQuestion, ""))
	r.post(qid('d'), coordID, 7_200_000, questionPost(4, goodQuestion, hour))
	r.post(qid('e'), coordID, 7_200_000, questionPost(5, goodQuestion, hour))
	r.post(qid('f'), coordID, 7_200_000, questionPost(6, goodQuestion, hour))
	r.post(qid('h'), aliceID, 1000, humanPost("Q4 yes", ""))
	r.post(qid('i'), coordID, 1000, threadPost(5, qid('e'), qid('e'), true))
	got := r.index.Reminders(testNow)
	want := []Reminder{
		{Key: qid('a') + "~humans", N: 1, QuestionID: qid('a'), Headline: "Which licence for the docs site?"},
		{Key: qid('f') + "~humans", N: 6, QuestionID: qid('f'), Headline: "Which licence for the docs site?"},
	}
	if len(got) != len(want) {
		t.Fatalf("Reminders = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reminder %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if ReminderKey(qid('a')) != want[0].Key {
		t.Errorf("ReminderKey = %q", ReminderKey(qid('a')))
	}
}
