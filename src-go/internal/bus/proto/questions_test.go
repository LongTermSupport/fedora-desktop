package proto

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	testAgent    = "@app.1+desk.podman:server.test"
	testQuestion = "Pick a name\nContext: we need one\nQuestion: which?\nOptions:\n1. foo\n2.\tbar"
)

func eventID(c byte) string { return "$" + strings.Repeat(string(c), 43) }

func TestCheckQuestionTextSections(t *testing.T) {
	headline, question, err := CheckQuestionText(testQuestion)
	if err != nil || headline != "Pick a name" || question != "which?" {
		t.Fatal(headline, question, err)
	}
	for _, text := range []string{
		"Context: first\nQuestion: q",
		"h\nContext: c\nQuestion: q\nContext: again",
		"h\nContext: c\nQuestion:  \n",
		"h\nContext: c\nQuestion: q\nOptions:\n2. b",
		"h\nContext: c\nQuestion: q\nOptions:",
		"h\nContext: c\nQuestion: q\nOptions: 1.x",
		"h\nContext: c\nQuestion: q\nOptions:\n1. a\n2. b\n3. c\n4. d\n5. e\n6. f\n7. g\n8. h\n9. i\n1. j",
		"h\nContext: c\nQuestion: q" + strings.Repeat("x", 8000),
	} {
		if _, _, err := CheckQuestionText(text); refusalReason(err) != ReasonBody {
			t.Errorf("%q: %v", text, err)
		}
	}
	if _, _, err := CheckQuestionText("h\nContext: c\nQuestion: $ANSIBLE_VAULT;1.1"); refusalReason(err) != RefuseSecret {
		t.Fatal(err)
	}
}

func questionEvent(n int64, id string) pyjson.Object {
	content := BuildQuestion(n, testQuestion, []string{"@bob:server.test", "@alice:server.test"}, []string{testAgent}, nil)
	return pyjson.Object{"type": "m.room.message", "event_id": id, "sender": testAgent, "content": content}
}

func TestParseQuestionRoundTrip(t *testing.T) {
	q, ok := ParseQuestion(questionEvent(3, eventID('a')), "server.test")
	if !ok || q.N != 3 || q.Headline != "Pick a name" || q.Question != "which?" || q.EventID != eventID('a') {
		t.Fatalf("%+v %v", q, ok)
	}
	event := questionEvent(3, eventID('a'))
	event["sender"] = "@alice:server.test"
	if _, ok := ParseQuestion(event, "server.test"); ok {
		t.Fatal("a human's message read as a question")
	}
}

func TestBuildThreadPost(t *testing.T) {
	if _, err := BuildThreadPost(ThreadPost{Text: "x"}); err == nil {
		t.Fatal("a post with no target was built")
	}
	post, err := BuildThreadPost(ThreadPost{Root: eventID('a'), Close: 2})
	if err != nil || post["body"] != "Q2 closed." || post["msgtype"] != noticeMsgtype {
		t.Fatal(post, err)
	}
	want := `{"event_id":"` + eventID('a') + `","is_falling_back":true,"m.in_reply_to":{"event_id":"` +
		eventID('a') + `"},"rel_type":"m.thread"}`
	if got := pyjson.Canonical(post["m.relates_to"]); got != want {
		t.Fatal(got)
	}
}
