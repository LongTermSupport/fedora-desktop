package proto

import (
	"slices"
	"strings"
	"testing"
)

// The forms DESIGN-questions-v2 adds (step 3): `why`, waiting posts, the link-back and the
// retitle edit, and what the index makes of each.

func TestComposeQuestionNeedsWhyAHuman(t *testing.T) {
	c := composer(t, coordID)
	out := mustSend(t)(c.Question(QuestionPost{Text: goodQuestion}))
	if at(t, out.Content, KeyQuestion, "why").Str() != WhyDecision {
		t.Errorf("the question's why: %s", out.Content.Compact())
	}
	v := receive(t, out, coordID, coordID)
	if v.Outcome != Index || v.Content.Question.Why != WhyDecision {
		t.Errorf("the receiver's why: %v %q %+v", v.Outcome, v.Reason, v.Content.Question)
	}
	noWhy := "Head\nContext: x\nQuestion: y"
	_, err := c.Question(QuestionPost{Text: noWhy})
	refusedWith(t, "no Why a human:", err, ReasonBody)
	_, err = c.Question(QuestionPost{Text: noWhy + "\nWhy a human: urgent"})
	refusedWith(t, "another word", err, ReasonBody)
	admin := composer(t, adminID)
	admin.Gate = nil
	out = mustSend(t)(admin.Question(QuestionPost{N: 7, Text: noWhy}))
	if _, has := at(t, out.Content, KeyQuestion).Get("why"); has {
		t.Errorf("a carried question has a why: %s", out.Content.Compact())
	}
	if v := receive(t, out, adminID, coordID); v.Outcome != Index {
		t.Errorf("a receiver requires why: %v %q", v.Outcome, v.Reason)
	}
}

func TestQuestionWhyIsOneOfTheFourWords(t *testing.T) {
	v := newQuestionRoom(t, 0).judged(qid('a'), coordID, 1, questionPost(1, goodQuestion, `,"why":"urgent"`))
	if v.Outcome != Drop || v.Reason != ReasonSchema {
		t.Errorf("a why of another word: %v %q", v.Outcome, v.Reason)
	}
}

func TestComposeThreadWaitsByDefault(t *testing.T) {
	c := asked(t)
	out := mustSend(t)(c.Thread(ThreadPost{N: 1, Text: "Still need this."}))
	if at(t, out.Content, "msgtype").Str() != MsgText || !slices.Equal(userIDs(t, out.Content), []string{aliceID}) ||
		!at(t, out.Content, KeyThread, "wait").Bool() {
		t.Errorf("a default thread post notifies and waits: %s", out.Content.Compact())
	}
	out = mustSend(t)(c.Thread(ThreadPost{N: 1, Text: "Noted.", Note: true}))
	if _, has := at(t, out.Content, KeyThread).Get("wait"); has || at(t, out.Content, "msgtype").Str() != MsgNotice {
		t.Errorf("a note is quiet and does not wait: %s", out.Content.Compact())
	}
	out = mustSend(t)(c.Thread(ThreadPost{N: 1, Answers: qid('h'), To: aliceID, Text: "MIT it is."}))
	if _, has := at(t, out.Content, KeyThread).Get("wait"); has || !slices.Equal(userIDs(t, out.Content), []string{aliceID}) {
		t.Errorf("a reply notifies its human and does not wait: %s", out.Content.Compact())
	}
	_, err := c.Thread(ThreadPost{N: 1, Answers: qid('h'), To: workerID, Text: "x"})
	refusedWith(t, "a reply mentioning an agent", err, ReasonTarget)
}

func TestQuestionWaitingSince(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	if got := r.question(1).WaitingMs; got != testNow-9000 {
		t.Fatalf("a question waits from its post: %d", got)
	}
	r.post(qid('h'), aliceID, 8000, humanPost("MIT", threadRel(qid('a'))))
	if got := r.question(1).WaitingMs; got != 0 {
		t.Errorf("an attached human post ends the wait: %d", got)
	}
	r.post(qid('n'), coordID, 7000, waitingPost(1, qid('a'), qid('h'), false))
	r.post(qid('o'), coordID, 6000, threadPost(1, qid('a'), qid('n'), false))
	if got := r.question(1).WaitingMs; got != testNow-7000 {
		t.Errorf("a waiting thread post starts the wait, a note leaves it: %d", got)
	}
	r.post(qid('c'), coordID, 5000, threadPost(1, qid('a'), qid('o'), true))
	if q := r.question(1); q.WaitingMs != 0 || !q.Closed {
		t.Errorf("a close ends the wait: %+v", q)
	}
}

func waitingPost(n int64, root, reply string, closes bool) string {
	return strings.Replace(threadPost(n, root, reply, closes), `"teamchat.thread":{"v":1,`, `"teamchat.thread":{"v":1,"wait":true,`, 1)
}

func TestThreadWaitAndLinkForms(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	for name, content := range map[string]string{
		"wait with close": waitingPost(1, qid('a'), qid('a'), true),
		"link in a thread": strings.Replace(threadPost(1, qid('a'), qid('a'), false), `"close":false`,
			`"close":false,"link":"`+qid('a')+`"`, 1),
		"link not an id":   linkBack("nope", qid('a')),
		"link without n":   `{"msgtype":"m.text","body":"x","m.relates_to":` + replyRel(qid('a')) + `,"teamchat.thread":{"v":1,"link":"` + qid('a') + `"}}`,
		"wait without n":   `{"msgtype":"m.text","body":"x","m.relates_to":` + replyRel(qid('a')) + `,"teamchat.thread":{"v":1,"wait":true}}`,
		"wait on the link": strings.Replace(linkBack(qid('a'), qid('a')), `"v":1`, `"v":1,"wait":true`, 1),
	} {
		if v := r.judged(qid('z'), coordID, 1, content); v.Outcome != Drop || v.Reason != ReasonSchema {
			t.Errorf("%s: %v %q", name, v.Outcome, v.Reason)
		}
	}
}

// linkBack is a link-back of Q1 to post, naming link.
func linkBack(link, post string) string {
	return `{"msgtype":"m.text","body":"Q1: answered in its thread","m.relates_to":` + replyRel(post) +
		`,"teamchat.thread":{"v":1,"n":1,"link":"` + link + `"}}`
}

// linked is a coordinator whose index holds Q1 (qid('a')) and a human's top-level `Q1:` post
// (qid('t')), answered in the thread by qid('c').
func linked(t *testing.T) *questionRoom {
	t.Helper()
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('t'), aliceID, 8000, humanPost("Q1: MIT", ""))
	r.post(qid('c'), coordID, 7000, threadPost(1, qid('a'), qid('t'), false))
	return r
}

func TestComposeLinkBack(t *testing.T) {
	r := linked(t)
	c := composer(t, coordID)
	c.Questions = r.index
	link := LinkPost{N: 1, Post: qid('t'), Link: qid('c'), To: aliceID, Permalink: Permalink(questionsRoom, qid('c'))}
	out := mustSend(t)(c.LinkBack(link))
	if !at(t, out.Content, keyRelatesTo).Equal(ReplyTo(qid('t')).Encode()) || at(t, out.Content, KeyThread, "link").Str() != qid('c') {
		t.Errorf("the link-back's relation and link: %s", out.Content.Compact())
	}
	want := "Q1: answered in its thread: https://matrix.to/#/" + questionsRoom + "/" + qid('c')
	if at(t, out.Content, "body").Str() != want || at(t, out.Content, "msgtype").Str() != MsgText ||
		!slices.Equal(userIDs(t, out.Content), []string{aliceID}) {
		t.Errorf("the link-back's body and mentions: %s", out.Content.Compact())
	}
	for name, bad := range map[string]LinkPost{
		"unattached post": {N: 1, Post: qid('z'), Link: qid('c'), To: aliceID, Permalink: "x"},
		"other question":  {N: 2, Post: qid('t'), Link: qid('c'), To: aliceID, Permalink: "x"},
		"to an agent":     {N: 1, Post: qid('t'), Link: qid('c'), To: workerID, Permalink: "x"},
		"link not an id":  {N: 1, Post: qid('t'), Link: "x", To: aliceID, Permalink: "x"},
	} {
		if _, err := c.LinkBack(bad); refusalOf(err) == "" {
			t.Errorf("%s: composed (%v)", name, err)
		}
	}
}

func TestLinkBackFold(t *testing.T) {
	r := linked(t)
	r.post(qid('h'), aliceID, 6000, humanPost("and the logo?", threadRel(qid('a'))))
	before := r.question(1)
	r.post(qid('l'), coordID, 5000, linkBack(qid('c'), qid('t')))
	after := r.question(1)
	if after.Latest != before.Latest || after.UnansweredCount() != before.UnansweredCount() || after.Closed {
		t.Errorf("a link-back changed the thread: %+v -> %+v", before, after)
	}
	if n := r.post(qid('r'), aliceID, 4000, humanPost("thanks", replyRel(qid('l')))); n != 1 {
		t.Errorf("a reply to the link-back attaches to %d", n)
	}
	if n := r.post(qid('s'), aliceID, 3000, humanPost("cheers", "")); n != 0 {
		t.Errorf("an unrelated post attaches to %d", n)
	}
	r.post(qid('m'), coordID, 2000, linkBack(qid('c'), qid('s')))
	if n := r.post(qid('x'), aliceID, 1000, humanPost("eh?", replyRel(qid('m')))); n != 0 {
		t.Errorf("a link-back to an unattached post was taken, and a reply to it attached to %d", n)
	}
}

// root is Q1's root event as the composer is given it: posted by the coordinator.
func root(t *testing.T) Event {
	t.Helper()
	return Event{ID: qid('a'), Type: MessageType, Sender: coordID, Content: mustParse(t, questionPost(1, goodQuestion, `,"why":"decision"`))}
}

var rest = strings.SplitN(goodQuestion, "\n", 2)[1]

func TestComposeRetitle(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, `,"why":"decision"`))
	c := composer(t, coordID)
	c.Questions = r.index
	out := mustSend(t)(c.Retitle(RetitlePost{N: 1, Headline: "Licence: MIT or CC-BY", Original: root(t)}))
	newBody := "Q1 Licence: MIT or CC-BY\n" + rest
	if at(t, out.Content, "msgtype").Str() != MsgNotice || len(userIDs(t, out.Content)) != 0 ||
		at(t, out.Content, "body").Str() != "* "+newBody {
		t.Errorf("the edit is quiet: %s", out.Content.Compact())
	}
	if !at(t, out.Content, keyRelatesTo).Equal(mustParse(t, `{"rel_type":"m.replace","event_id":"`+qid('a')+`"}`)) {
		t.Errorf("the edit's relation: %s", out.Content.Compact())
	}
	if at(t, out.Content, keyNewContent, "body").Str() != newBody || !at(t, out.Content, keyNewContent, KeyQuestion).Equal(at(t, root(t).Content, KeyQuestion)) {
		t.Errorf("the new content: %s", out.Content.Compact())
	}
	v := r.judged(qid('e'), coordID, 100, string(out.Content.Compact()))
	if v.Outcome != Index || v.Content.Retitles != qid('a') || !r.index.Feed(v) {
		t.Fatalf("the retitle was not folded: %v %q", v.Outcome, v.Reason)
	}
	if got := r.question(1).Headline; got != "Licence: MIT or CC-BY" {
		t.Errorf("the headline is %q", got)
	}
	if r.index.Feed(v) {
		t.Error("the same edit folded twice")
	}
	r.post(qid('k'), coordID, 50, threadPost(1, qid('a'), qid('a'), true))
	out = mustSend(t)(c.Retitle(RetitlePost{N: 1, Headline: "Licence: MIT or CC-BY", Original: root(t)}))
	if at(t, out.Content, keyNewContent, "body").Str() != "Q1 ✅ CLOSED — Licence: MIT or CC-BY\n"+rest {
		t.Errorf("a closed question's title: %s", at(t, out.Content, keyNewContent, "body").Compact())
	}
}

func TestComposeRetitleRefuses(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, `,"why":"decision"`))
	carried := questionPost(2, strings.Replace(goodQuestion, "MIT or", "GPL or", 1), "")
	r.post(qid('b'), adminID, 8000, carried)
	c := composer(t, coordID)
	c.Questions = r.index
	other := Event{ID: qid('b'), Sender: adminID, Content: mustParse(t, carried)}
	for name, tc := range map[string]struct {
		post   RetitlePost
		reason string
	}{
		"not kept":     {RetitlePost{N: 9, Headline: "x", Original: root(t)}, ReasonTarget},
		"another seat": {RetitlePost{N: 2, Headline: "x", Original: other}, ReasonTarget},
		"wrong event":  {RetitlePost{N: 1, Headline: "x", Original: other}, ReasonTarget},
		"blank":        {RetitlePost{N: 1, Headline: " ", Original: root(t)}, ReasonBody},
		"a section":    {RetitlePost{N: 1, Headline: "Context: x", Original: root(t)}, ReasonBody},
		"secret":       {RetitlePost{N: 1, Headline: ownSample, Original: root(t)}, ReasonSecret},
		"two lines":    {RetitlePost{N: 1, Headline: "a\nb", Original: root(t)}, ReasonBody},
		"too long":     {RetitlePost{N: 1, Headline: strings.Repeat("x", 121), Original: root(t)}, ReasonBody},
	} {
		_, err := c.Retitle(tc.post)
		refusedWith(t, name, err, tc.reason)
		if name == "another seat" && (err == nil || !strings.Contains(err.Error(), "only its sender can edit it")) {
			t.Errorf("another seat's refusal: %v", err)
		}
	}
	w := composer(t, workerID)
	w.Questions = r.index
	_, err := w.Retitle(RetitlePost{N: 1, Headline: "x", Original: root(t)})
	refusedWith(t, "a worker", err, ReasonRole)
}

func TestRetitleFoldIsChecked(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, `,"why":"decision"`))
	edit := func(id, sender, body, obj string) Verdict {
		content := `{"msgtype":"m.notice","body":"* x","m.mentions":{},"m.new_content":{"msgtype":"m.text","body":` + jsonString(body) +
			`,"teamchat.question":` + obj + `},"m.relates_to":{"rel_type":"m.replace","event_id":"` + qid('a') + `"}}`
		return r.judged(id, sender, 1, content)
	}
	good := `{"v":1,"n":1,"why":"decision"}`
	for name, v := range map[string]Verdict{
		"another sender":   edit(qid('d'), workerID, "Q1 New title\n"+rest, good),
		"another question": edit(qid('e'), coordID, "Q1 New title\nContext: c\nQuestion: something else\nWhy a human: decision", good),
		"another why":      edit(qid('f'), coordID, "Q1 New title\n"+rest, `{"v":1,"n":1,"why":"preference"}`),
		"another n":        edit(qid('g'), coordID, "Q2 New title\n"+rest, `{"v":1,"n":2,"why":"decision"}`),
		"asked_by added":   edit(qid('h'), coordID, "Q1 New title\n"+rest, `{"v":1,"n":1,"why":"decision","asked_by":["`+workerID+`"]}`),
	} {
		if v.Outcome == Index && r.index.Feed(v) {
			t.Errorf("%s: the edit was folded", name)
		}
	}
	if got := r.question(1).Headline; got != "Which licence for the docs site?" {
		t.Errorf("a refused edit changed the headline to %q", got)
	}
	threaded := `{"msgtype":"m.notice","body":"* x","m.new_content":{"msgtype":"m.text","body":` + jsonString("Q1 New title\n"+rest) +
		`,"teamchat.question":` + good + `},"m.relates_to":{"rel_type":"m.replace","event_id":"` + qid('a') + `","is_falling_back":false}}`
	if v := r.judged(qid('i'), coordID, 1, threaded); v.Outcome != Drop || v.Reason != ReasonEdit {
		t.Errorf("an edit whose relation is not exactly a replace: %v %q", v.Outcome, v.Reason)
	}
}

func TestQuestionsStateKeepsWhyFromAndWaiting(t *testing.T) {
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, `,"why":"physical"`))
	q := r.question(1)
	if q.Why != WhyPhysical || q.From != coordID || q.WaitingMs != testNow-9000 {
		t.Fatalf("the indexed question: %+v", q)
	}
	back, err := LoadQuestionIndex(testServer, 0, r.index.State())
	if err != nil || stateJSON(t, back) != stateJSON(t, r.index) {
		t.Errorf("the state does not round-trip: %v", err)
	}
	bad := r.index.State()
	bad.Questions[0].Why = "urgent"
	if err := bad.Check(testServer); err == nil {
		t.Error("a stored why of another word passed")
	}
	bad = r.index.State()
	bad.Questions[0].WaitingMs = -1
	if err := bad.Check(testServer); err == nil {
		t.Error("a negative waiting_ms passed")
	}
}

// A retitle's root as the homeserver gives it (A 10.4): DecodeEvent reads it by rule 02, and a
// redacted root or one with no id is refused.
func TestDecodeEventReadsAFetchedRoot(t *testing.T) {
	raw := `{"event_id":"` + qid('a') + `","type":"m.room.message","sender":"` + coordID + `","origin_server_ts":9000,"content":` + questionPost(1, goodQuestion, `,"why":"decision"`) + `}`
	ev, ok := DecodeEvent(mustParse(t, raw))
	if !ok || ev.ID != qid('a') || ev.Sender != coordID || ev.OriginTs != 9000 {
		t.Fatalf("decoded %+v, %v", ev, ok)
	}
	if _, ok := DecodeEvent(mustParse(t, `{"type":"m.room.message","sender":"`+coordID+`","origin_server_ts":1,"content":{}}`)); ok {
		t.Error("an event with no id decoded")
	}
	redacted := `{"event_id":"` + qid('a') + `","type":"m.room.message","sender":"` + coordID + `","origin_server_ts":1,"content":{},"unsigned":{"redacted_because":{}}}`
	if _, ok := DecodeEvent(mustParse(t, redacted)); ok {
		t.Error("a redacted event decoded")
	}
}
