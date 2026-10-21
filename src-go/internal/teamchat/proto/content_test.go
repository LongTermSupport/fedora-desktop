package proto

import (
	"slices"
	"strings"
	"testing"
)

var (
	pingEvent  = "$" + strings.Repeat("p", 43)
	replyEvent = "$" + strings.Repeat("r", 43)
	otherEvent = "$" + strings.Repeat("o", 43)
)

func decode(t *testing.T, keys []string, s string) (Content, string) {
	t.Helper()
	return DecodeContent(mustParse(t, s), keys)
}

func mustDecode(t *testing.T, keys []string, s string) Content {
	t.Helper()
	c, reason := decode(t, keys, s)
	if reason != "" {
		t.Fatalf("DecodeContent refused %q: %s", reason, s)
	}
	return c
}

func mustHuman(t *testing.T, s string) Human {
	t.Helper()
	h, reason := DecodeHuman(mustParse(t, s))
	if reason != "" {
		t.Fatalf("DecodeHuman refused %q: %s", reason, s)
	}
	return h
}

func TestDecodeContentAcceptsEachKey(t *testing.T) {
	cases := map[string]struct {
		keys []string
		json string
	}{
		"ping": {MainKeys, `{"msgtype":"m.notice","body":"x","teamchat.ping":{"v":1,"verb":"review","to":["` + workerID + `","` + coordID + `"],"ref":"commit:example-org/myrepo@` + testSHA + `"}}`},
		"text with reply": {MainKeys, `{"msgtype":"m.text","body":"x","m.relates_to":{"m.in_reply_to":{"event_id":"` + replyEvent + `"}},
"teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"hello","later":true}}`},
		"notice":   {MainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"left","subject":"` + workerID + `","reason":"exit","to":["` + aliceID + `"]}}`},
		"moved":    {AdminMainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"moved","to":["` + workerID + `"]}}`},
		"added":    {AdminMainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"added","subject":"` + workerID + `","role":"worker","to":["` + aliceID + `"]}}`},
		"report":   {ReportKeys, `{"body":"Merged\nmore","m.mentions":{},"teamchat.report":{"v":1}}`},
		"question": {QuestionKeys, `{"body":"Q3 Which?","teamchat.question":{"v":1,"n":3,"asked_by":["` + workerID + `"],"re":["` + replyEvent + `"],"remind_after_s":600}}`},
		"thread": {QuestionKeys, `{"body":"ok","m.relates_to":{"rel_type":"m.thread","event_id":"` + pingEvent + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` + replyEvent + `"}},
"teamchat.thread":{"v":1,"n":3,"close":true}}`},
		"unattached reply": {QuestionKeys, `{"body":"which?","m.relates_to":{"m.in_reply_to":{"event_id":"` + replyEvent + `"}},"teamchat.thread":{"v":1}}`},
		"carried": {AdminMainKeys, `{"body":"x","teamchat.carried":{"v":1,"kind":"ping","verb":"review","ref":"issue:example-org/myrepo#12",
"from":"` + workerID + `","sent_ms":5,"v1_event":"` + otherEvent + `","to":["` + coordID + `"],"text":"please","possible_duplicate":true}}`},
	}
	for name, tc := range cases {
		if _, reason := decode(t, tc.keys, tc.json); reason != "" {
			t.Errorf("%s: refused %q", name, reason)
		}
	}
}

func TestDecodeContentFields(t *testing.T) {
	c := mustDecode(t, MainKeys, `{"body":"x","teamchat.ping":{"v":1,"verb":"ack","to":["`+workerID+`","`+coordID+`"]},
"m.relates_to":{"rel_type":"m.thread","event_id":"`+pingEvent+`","is_falling_back":true,"m.in_reply_to":{"event_id":"`+pingEvent+`"}}}`)
	if c.Key != KeyPing || c.Ping.Verb != VerbAck || !slices.Equal(c.Ping.To, []string{coordID, workerID}) || !c.Relation.IsAnswer() || c.Relation.Root != pingEvent {
		t.Fatalf("ping = %+v", c)
	}
	c = mustDecode(t, AdminMainKeys, `{"body":"x","teamchat.carried":{"v":1,"kind":"text","from":"`+aliceID+`","sent_ms":7,"v1_event":"`+otherEvent+`","to":["`+coordID+`"],"text":"hi"}}`)
	if c.Carried.Kind != "text" || c.Carried.From != aliceID || c.Carried.SentMs != 7 || c.Carried.To != coordID || c.Carried.PossibleDuplicate {
		t.Fatalf("carried = %+v", c.Carried)
	}
	c = mustDecode(t, QuestionKeys, `{"body":"Q2 x","teamchat.question":{"v":1,"n":2}}`)
	if c.Question.N != 2 || c.Question.RemindAfterS != 0 || c.Body != "Q2 x" {
		t.Fatalf("question = %+v", c)
	}
}

func TestDecodeContentRefuses(t *testing.T) {
	ping := func(obj string) string { return `{"body":"x","teamchat.ping":` + obj + `}` }
	text := func(extra string) string {
		return `{"body":"x"` + extra + `,"teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"x"}}`
	}
	thread := func(rel, obj string) string {
		return `{"body":"x","m.relates_to":` + rel + `,"teamchat.thread":` + obj + `}`
	}
	threadRel := `{"rel_type":"m.thread","event_id":"` + pingEvent + `","is_falling_back":false,"m.in_reply_to":{"event_id":"` + replyEvent + `"}}`
	replyRel := `{"m.in_reply_to":{"event_id":"` + replyEvent + `"}}`
	big := strings.Repeat("a", MaxContent)
	bigText := strings.Repeat("a", MaxText+1)
	cases := map[string]struct {
		keys       []string
		json, want string
	}{
		"no typed key":          {MainKeys, `{"body":"x"}`, ReasonSchema},
		"two typed keys":        {MainKeys, `{"body":"x","teamchat.ping":{"v":1},"teamchat.text":{"v":1}}`, ReasonSchema},
		"other room's key":      {MainKeys, `{"body":"x","teamchat.report":{"v":1}}`, ReasonSchema},
		"typed not object":      {MainKeys, `{"body":"x","teamchat.text":[]}`, ReasonSchema},
		"v not integer":         {MainKeys, ping(`{"v":"1"}`), ReasonSchema},
		"v absent":              {MainKeys, ping(`{}`), ReasonSchema},
		"v two":                 {MainKeys, ping(`{"v":2,"verb":7}`), ReasonVersion},
		"features":              {MainKeys, ping(`{"v":1,"features":[]}`), ReasonVersion},
		"version before size":   {MainKeys, `{"body":"` + big + `","teamchat.text":{"v":2}}`, ReasonVersion},
		"content size":          {MainKeys, `{"body":"` + big + `","teamchat.text":{"v":1}}`, ReasonSize},
		"ping object size":      {MainKeys, ping(`{"v":1,"verb":"halt","to":["` + strings.Repeat("a", MaxPingObject) + `"]}`), ReasonSize},
		"new content":           {MainKeys, `{"body":"x","teamchat.text":{"v":1,"x":[{"m.new_content":{}}]}}`, ReasonEdit},
		"replace":               {MainKeys, text(`,"m.relates_to":{"rel_type":"m.replace","event_id":"` + replyEvent + `"}`), ReasonEdit},
		"format":                {MainKeys, text(`,"format":"org.matrix.custom.html"`), ReasonSchema},
		"body absent":           {MainKeys, `{"teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"x"}}`, ReasonSchema},
		"body not string":       {MainKeys, `{"body":1,"teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"x"}}`, ReasonSchema},
		"relation extra key":    {MainKeys, text(`,"m.relates_to":{"m.in_reply_to":{"event_id":"` + replyEvent + `"},"x":1}`), ReasonSchema},
		"relation bad id":       {MainKeys, text(`,"m.relates_to":{"m.in_reply_to":{"event_id":"$x"}}`), ReasonSchema},
		"relation not object":   {MainKeys, text(`,"m.relates_to":[]`), ReasonSchema},
		"reply not object":      {MainKeys, text(`,"m.relates_to":{"m.in_reply_to":1}`), ReasonSchema},
		"thread rel_type":       {QuestionKeys, thread(`{"rel_type":"m.annotation","event_id":"`+pingEvent+`","is_falling_back":true,"m.in_reply_to":{"event_id":"`+pingEvent+`"}}`, `{"v":1,"n":1}`), ReasonSchema},
		"thread falling string": {QuestionKeys, thread(`{"rel_type":"m.thread","event_id":"`+pingEvent+`","is_falling_back":"yes","m.in_reply_to":{"event_id":"`+pingEvent+`"}}`, `{"v":1,"n":1}`), ReasonSchema},
		"text in thread":        {MainKeys, text(`,"m.relates_to":` + threadRel), ReasonSchema},
		"ping to absent":        {MainKeys, ping(`{"v":1,"verb":"halt"}`), ReasonSchema},
		"ping to not strings":   {MainKeys, ping(`{"v":1,"verb":"halt","to":[1]}`), ReasonSchema},
		"ping ref not string":   {MainKeys, ping(`{"v":1,"verb":"halt","to":[],"ref":1}`), ReasonSchema},
		"text empty":            {MainKeys, `{"body":"x","teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":""}}`, ReasonSchema},
		"text too long":         {MainKeys, `{"body":"x","teamchat.text":{"v":1,"to":["` + aliceID + `"],"text":"` + bigText + `"}}`, ReasonSize},
		"notice unknown":        {MainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"joined","subject":"` + workerID + `","to":[]}}`, ReasonSchema},
		"notice no subject":     {MainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"back","to":[]}}`, ReasonSchema},
		"moved subject":         {AdminMainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"moved","subject":"` + workerID + `","to":[]}}`, ReasonSchema},
		"left no reason":        {MainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"left","subject":"` + workerID + `","to":[]}}`, ReasonSchema},
		"left bad reason":       {MainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"left","subject":"` + workerID + `","reason":"crash","to":[]}}`, ReasonSchema},
		"role probe":            {AdminMainKeys, `{"body":"x","teamchat.notice":{"v":1,"event":"role","subject":"` + workerID + `","role":"probe","to":[]}}`, ReasonSchema},
		"notice relation":       {MainKeys, `{"body":"x","m.relates_to":` + replyRel + `,"teamchat.notice":{"v":1,"event":"back","subject":"` + workerID + `","to":[]}}`, ReasonSchema},
		"report relation":       {ReportKeys, `{"body":"x","m.relates_to":` + replyRel + `,"teamchat.report":{"v":1}}`, ReasonSchema},
		"question n zero":       {QuestionKeys, `{"body":"Q0 x","teamchat.question":{"v":1,"n":0}}`, ReasonSchema},
		"question n huge":       {QuestionKeys, `{"body":"x","teamchat.question":{"v":1,"n":1000000000}}`, ReasonSchema},
		"question prefix":       {QuestionKeys, `{"body":"Q4 x","teamchat.question":{"v":1,"n":3}}`, ReasonSchema},
		"question big":          {QuestionKeys, `{"body":"Q1 ` + strings.Repeat("a", MaxQuestionBody) + `","teamchat.question":{"v":1,"n":1}}`, ReasonSize},
		"question remind":       {QuestionKeys, `{"body":"Q1 x","teamchat.question":{"v":1,"n":1,"remind_after_s":599}}`, ReasonSchema},
		"question asked_by":     {QuestionKeys, `{"body":"Q1 x","teamchat.question":{"v":1,"n":1,"asked_by":` + idList(MaxAskedBy+1) + `}}`, ReasonSchema},
		"question re":           {QuestionKeys, `{"body":"Q1 x","teamchat.question":{"v":1,"n":1,"re":["$x"]}}`, ReasonSchema},
		"question relation":     {QuestionKeys, `{"body":"Q1 x","m.relates_to":` + replyRel + `,"teamchat.question":{"v":1,"n":1}}`, ReasonSchema},
		"thread no relation":    {QuestionKeys, `{"body":"x","teamchat.thread":{"v":1,"n":1}}`, ReasonSchema},
		"thread with n reply":   {QuestionKeys, thread(replyRel, `{"v":1,"n":1}`), ReasonSchema},
		"unattached in thread":  {QuestionKeys, thread(threadRel, `{"v":1}`), ReasonSchema},
		"unattached close":      {QuestionKeys, thread(replyRel, `{"v":1,"close":true}`), ReasonSchema},
		"thread big":            {QuestionKeys, `{"body":"` + strings.Repeat("a", MaxThreadBody+1) + `","m.relates_to":` + threadRel + `,"teamchat.thread":{"v":1,"n":1}}`, ReasonSize},
		"carried kind":          {AdminMainKeys, carried(`"kind":"other"`), ReasonSchema},
		"carried verb text":     {AdminMainKeys, carried(`"kind":"text","verb":"review"`), ReasonSchema},
		"carried ref text":      {AdminMainKeys, carried(`"kind":"text","ref":"issue:example-org/myrepo#1"`), ReasonSchema},
		"carried no verb":       {AdminMainKeys, carried(`"kind":"ping"`), ReasonSchema},
		"carried two to":        {AdminMainKeys, carriedTo(`"` + coordID + `","` + workerID + `"`), ReasonSchema},
		"carried v1 event":      {AdminMainKeys, `{"body":"x","teamchat.carried":{"v":1,"kind":"text","from":"` + aliceID + `","sent_ms":1,"v1_event":"$x","to":["` + coordID + `"],"text":"x"}}`, ReasonSchema},
		"carried sent_ms":       {AdminMainKeys, `{"body":"x","teamchat.carried":{"v":1,"kind":"text","from":"` + aliceID + `","sent_ms":-1,"v1_event":"` + otherEvent + `","to":["` + coordID + `"],"text":"x"}}`, ReasonSchema},
		"carried text big":      {AdminMainKeys, carried(`"kind":"text","text":"` + bigText + `"`), ReasonSize},
		"carried relation":      {AdminMainKeys, `{"body":"x","m.relates_to":` + replyRel + `,"teamchat.carried":{"v":1,"kind":"text","from":"` + aliceID + `","sent_ms":1,"v1_event":"` + otherEvent + `","to":["` + coordID + `"]}}`, ReasonSchema},
	}
	for name, tc := range cases {
		if _, reason := decode(t, tc.keys, tc.json); reason != tc.want {
			t.Errorf("%s: reason %q, want %q", name, reason, tc.want)
		}
	}
}

// carried is a carried item to the coordinator with these fields first; a later duplicate key
// would be refused by the strict decoder, so fields never repeats from, to or text.
func carried(fields string) string {
	return `{"body":"x","teamchat.carried":{"v":1,` + fields + `,"from":"` + aliceID + `","sent_ms":1,"v1_event":"` + otherEvent + `","to":["` + coordID + `"]}}`
}

func carriedTo(to string) string {
	return `{"body":"x","teamchat.carried":{"v":1,"kind":"text","from":"` + aliceID + `","sent_ms":1,"v1_event":"` + otherEvent + `","to":[` + to + `]}}`
}

func idList(n int) string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, `"`+UserID("w"+string(rune('a'+i))+".1+desk.podman", testServer)+`"`)
	}
	return "[" + strings.Join(ids, ",") + "]"
}

func TestMemberNotice(t *testing.T) {
	for _, event := range NoticeEvents {
		if want := event == EventLeft || event == EventDisconnected || event == EventBack; MemberNotice(event) != want {
			t.Errorf("MemberNotice(%q) = %v", event, !want)
		}
	}
}

func TestRelationEncodeRoundTrips(t *testing.T) {
	for _, rel := range []Relation{ReplyTo(replyEvent), AnswerTo(pingEvent), InThread(pingEvent, replyEvent, false), InThread(pingEvent, pingEvent, true)} {
		got, good := decodeRelation(rel.Encode())
		if !good || got != rel {
			t.Fatalf("relation %+v round-tripped to %+v", rel, got)
		}
	}
	if !AnswerTo(pingEvent).IsAnswer() || InThread(pingEvent, replyEvent, true).IsAnswer() || ReplyTo(pingEvent).IsAnswer() {
		t.Fatal("IsAnswer wrong")
	}
}

func TestDecodeHuman(t *testing.T) {
	h := mustHuman(t, `{"msgtype":"m.text","body":"> <@bob:x> earlier\n> more\n\nreal answer",
"m.mentions":{"user_ids":["`+workerID+`",3],"room":true},"m.relates_to":{"m.in_reply_to":{"event_id":"`+replyEvent+`"}}}`)
	if h.Body != "real answer" || h.ReplyTo != replyEvent || !h.MentionRoom || !slices.Equal(h.Mentions, []string{workerID}) {
		t.Fatalf("reply = %+v", h)
	}
	if h = mustHuman(t, `{"msgtype":"m.text","body":"> <@bob:x> quoted, no reply"}`); h.Body != "> <@bob:x> quoted, no reply" {
		t.Fatalf("a quote without a reply was cut: %q", h.Body)
	}
	h = mustHuman(t, `{"msgtype":"m.text","body":"> plain quote\nanswer","m.relates_to":{"m.in_reply_to":{"event_id":"`+replyEvent+`"}}}`)
	if h.Body != "> plain quote\nanswer" {
		t.Fatalf("a quote without the fallback form was cut: %q", h.Body)
	}
	h = mustHuman(t, `{"msgtype":"m.text","body":"in thread","m.relates_to":{"rel_type":"m.thread","event_id":"`+pingEvent+`",
"is_falling_back":true,"m.in_reply_to":{"event_id":"`+replyEvent+`"}}}`)
	if h.ThreadRoot != pingEvent || h.ReplyTo != "" {
		t.Fatalf("thread fallback = %+v", h)
	}
	h = mustHuman(t, `{"msgtype":"m.text","body":"in thread","m.relates_to":{"rel_type":"m.thread","event_id":"`+pingEvent+`",
"is_falling_back":false,"m.in_reply_to":{"event_id":"`+replyEvent+`"}}}`)
	if h.ThreadRoot != pingEvent || h.ReplyTo != replyEvent {
		t.Fatalf("thread reply = %+v", h)
	}
	h = mustHuman(t, `{"msgtype":"m.text","body":"* fixed","m.new_content":{"msgtype":"m.text","body":"fixed",
"m.mentions":{"user_ids":["`+coordID+`"]}},"m.relates_to":{"rel_type":"m.replace","event_id":"`+otherEvent+`"}}`)
	if h.Revises != otherEvent || h.Body != "fixed" || !slices.Equal(h.Mentions, []string{coordID}) {
		t.Fatalf("edit = %+v", h)
	}
}

func TestDecodeHumanRefuses(t *testing.T) {
	cases := map[string]struct{ json, want string }{
		"emote":          {`{"msgtype":"m.emote","body":"waves"}`, IgnoreType},
		"no msgtype":     {`{"body":"x"}`, IgnoreType},
		"body absent":    {`{"msgtype":"m.text"}`, ReasonSchema},
		"too big":        {`{"msgtype":"m.text","body":"` + strings.Repeat("a", MaxHumanBody+1) + `"}`, ReasonSize},
		"empty":          {`{"msgtype":"m.text","body":"  "}`, IgnoreEmpty},
		"only fallback":  {`{"msgtype":"m.text","body":"> <@a:b> x\n","m.relates_to":{"m.in_reply_to":{"event_id":"` + replyEvent + `"}}}`, IgnoreEmpty},
		"edit bad id":    {`{"msgtype":"m.text","body":"* x","m.new_content":{"msgtype":"m.text","body":"x"},"m.relates_to":{"rel_type":"m.replace","event_id":"$x"}}`, ReasonEdit},
		"edit no new":    {`{"msgtype":"m.text","body":"* x","m.relates_to":{"rel_type":"m.replace","event_id":"` + otherEvent + `"}}`, ReasonEdit},
		"edit new emote": {`{"msgtype":"m.text","body":"* x","m.new_content":{"msgtype":"m.emote","body":"x"},"m.relates_to":{"rel_type":"m.replace","event_id":"` + otherEvent + `"}}`, ReasonEdit},
	}
	for name, tc := range cases {
		if _, reason := DecodeHuman(mustParse(t, tc.json)); reason != tc.want {
			t.Errorf("%s: reason %q, want %q", name, reason, tc.want)
		}
	}
}
