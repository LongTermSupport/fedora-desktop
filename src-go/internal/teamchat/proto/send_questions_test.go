package proto

import (
	"slices"
	"testing"
)

// asked is a coordinator whose index holds Q1 (event qid('a')), a coordinator thread post in it
// (qid('c')), a human post attached to it (qid('h')) and an unattached human post (qid('u')).
func asked(t *testing.T) Composer {
	t.Helper()
	r := newQuestionRoom(t, 0)
	r.post(qid('a'), coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(qid('c'), coordID, 8000, threadPost(1, qid('a'), qid('a'), false))
	r.post(qid('h'), aliceID, 7000, humanPost("MIT", threadRel(qid('a'))))
	r.post(qid('u'), aliceID, 6000, humanPost("what is this about?", ""))
	c := composer(t, coordID)
	c.Questions = r.index
	return c
}

func TestComposeQuestion(t *testing.T) {
	c := composer(t, coordID)
	q := QuestionPost{Text: goodQuestion, AskedBy: []string{workerID}, Re: []string{otherEvent}, RemindAfterS: 3600}
	out := mustSend(t)(c.Question(q))
	if out.Purpose != PurposeQuestions || at(t, out.Content, "body").Str() != "Q1 "+goodQuestion {
		t.Errorf("question = %s", out.Content.Compact())
	}
	if at(t, out.Content, "msgtype").Str() != MsgText || !slices.Equal(userIDs(t, out.Content), []string{aliceID}) {
		t.Errorf("a question notifies the humans: %s", out.Content.Compact())
	}
	v := receive(t, out, coordID, coordID)
	if v.Outcome != Index || !c.Questions.Feed(v) || c.Questions.Next() != 2 {
		t.Errorf("the index did not count the question: %v %q", v.Outcome, v.Reason)
	}
	_, err := c.Question(QuestionPost{Text: "Again\nContext: x\nQuestion: MIT  or\nCC-BY?"})
	refusedWith(t, "the same question again", err, ReasonDuplicate)
}

func TestComposeQuestionRefuses(t *testing.T) {
	cases := map[string]struct {
		self   string
		q      QuestionPost
		reason string
	}{
		"worker":     {workerID, QuestionPost{Text: goodQuestion}, ReasonRole},
		"probe":      {probeID, QuestionPost{Text: goodQuestion}, ReasonRole},
		"parked":     {parkedID, QuestionPost{Text: goodQuestion}, ReasonSender},
		"form":       {coordID, QuestionPost{Text: "Head\nQuestion: x"}, ReasonBody},
		"secret":     {coordID, QuestionPost{Text: "Head\nContext: " + ownSample + "\nQuestion: x"}, ReasonSecret},
		"asked by":   {coordID, QuestionPost{Text: goodQuestion, AskedBy: []string{aliceID}}, ReasonTarget},
		"numbered":   {coordID, QuestionPost{N: 4, Text: goodQuestion}, ReasonSchema},
		"remind":     {coordID, QuestionPost{Text: goodQuestion, RemindAfterS: 60}, ReasonSchema},
		"admin no n": {adminID, QuestionPost{Text: goodQuestion}, ReasonSchema},
	}
	for name, tc := range cases {
		_, err := composer(t, tc.self).Question(tc.q)
		refusedWith(t, name, err, tc.reason)
	}
}

func TestComposeAdminQuestion(t *testing.T) {
	c := composer(t, adminID)
	c.Gate = nil
	out := mustSend(t)(c.Question(QuestionPost{N: 7, Text: goodQuestion}))
	index := NewQuestionIndex(testServer, 0)
	if v := receive(t, out, adminID, coordID); !index.Feed(v) || index.Next() != 8 {
		t.Errorf("the carried question was not counted: %v %q", v.Outcome, v.Reason)
	}
	if err := c.Admit(out); err != nil {
		t.Errorf("Admit: %v", err)
	}
}

func TestComposeThreadPost(t *testing.T) {
	c := asked(t)
	out := mustSend(t)(c.Thread(ThreadPost{N: 1, Text: "Relayed."}))
	if !at(t, out.Content, keyRelatesTo).Equal(InThread(qid('a'), qid('h'), true).Encode()) {
		t.Errorf("the fallback names the thread's latest: %s", at(t, out.Content, keyRelatesTo).Compact())
	}
	if at(t, out.Content, "msgtype").Str() != MsgNotice || len(at(t, out.Content, keyMentions).Members()) != 0 {
		t.Errorf("a quiet follow-up = %s", out.Content.Compact())
	}
	if v := receive(t, out, coordID, coordID); v.Outcome != Index || !c.Questions.Feed(v) {
		t.Errorf("the index did not take the post: %v %q", v.Outcome, v.Reason)
	}
	out = mustSend(t)(c.Thread(ThreadPost{N: 1, Answers: qid('h'), Ask: true, Close: true}))
	if !at(t, out.Content, keyRelatesTo).Equal(InThread(qid('a'), qid('h'), false).Encode()) {
		t.Errorf("an answer names the post: %s", at(t, out.Content, keyRelatesTo).Compact())
	}
	if at(t, out.Content, "body").Str() != "Q1 closed." || !at(t, out.Content, KeyThread, "close").Bool() {
		t.Errorf("a closing post = %s", out.Content.Compact())
	}
	if at(t, out.Content, "msgtype").Str() != MsgText || !slices.Equal(userIDs(t, out.Content), []string{aliceID}) {
		t.Errorf("a post that asks again notifies: %s", out.Content.Compact())
	}
}

func TestComposeUnattachedReply(t *testing.T) {
	c := asked(t)
	out := mustSend(t)(c.Thread(ThreadPost{Answers: qid('u'), Text: "Which question do you mean?"}))
	if !at(t, out.Content, keyRelatesTo).Equal(ReplyTo(qid('u')).Encode()) {
		t.Errorf("the reply = %s", out.Content.Compact())
	}
	if _, has := at(t, out.Content, KeyThread).Get("n"); has {
		t.Errorf("an unattached reply has an n: %s", out.Content.Compact())
	}
	if v := receive(t, out, coordID, coordID); v.Outcome != Index || c.Questions.Feed(v) {
		t.Errorf("the index changed for an unattached reply: %v %q", v.Outcome, v.Reason)
	}
}

func TestComposeThreadPostRefuses(t *testing.T) {
	cases := map[string]struct {
		post   ThreadPost
		reason string
	}{
		"unknown question":    {ThreadPost{N: 4, Text: "x"}, ReasonTarget},
		"answers unattached":  {ThreadPost{N: 1, Answers: qid('u'), Text: "x"}, ReasonTarget},
		"answers unknown":     {ThreadPost{N: 1, Answers: qid('z'), Text: "x"}, ReasonTarget},
		"reply to attached":   {ThreadPost{Answers: qid('h'), Text: "x"}, ReasonTarget},
		"reply closes":        {ThreadPost{Answers: qid('u'), Text: "x", Close: true}, ReasonTarget},
		"neither":             {ThreadPost{Text: "x"}, ReasonTarget},
		"blank":               {ThreadPost{N: 1, Text: " "}, ReasonBody},
		"secret":              {ThreadPost{N: 1, Text: ownSample}, ReasonSecret},
		"secret in the reply": {ThreadPost{Answers: qid('u'), Text: ownSample}, ReasonSecret},
	}
	for name, tc := range cases {
		_, err := asked(t).Thread(tc.post)
		refusedWith(t, name, err, tc.reason)
	}
	worker := composer(t, workerID)
	_, err := worker.Thread(ThreadPost{N: 1, Text: "x"})
	refusedWith(t, "a worker", err, ReasonRole)
}
