package proto

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func testEvent(sender string, content pyjson.Object) pyjson.Object {
	return pyjson.Object{
		"type": "m.room.message", "event_id": "$" + strings.Repeat("E", 43), "sender": sender,
		"origin_server_ts": pyjson.Number{Lit: "5", Kind: pyjson.Int}, "content": content,
	}
}

func TestValidateEventRefusesAnotherRoom(t *testing.T) {
	if _, err := ValidateEvent(pyjson.Object{}, testContext(), Receiver{Self: testCoord, Room: "reports"}); !errors.Is(err, ErrRoom) {
		t.Fatalf("%v", err)
	}
}

func TestValidateEventAcceptsAHumanMessage(t *testing.T) {
	content := pyjson.Object{
		"msgtype": "m.text", "body": "> quoted\n\nhi",
		"m.mentions":   pyjson.Object{"user_ids": []any{testCoord}},
		"m.relates_to": pyjson.Object{"m.in_reply_to": pyjson.Object{}},
	}
	got, err := ValidateEvent(testEvent(testAlice, content), testContext(), Receiver{Self: testCoord, HumanText: true, Room: ReceiveMain})
	if err != nil || got.Verdict != Accept || got.Human == nil || got.Human.Text != "hi" || got.Human.TS != 5 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestUnheardIsAHumanMessageForNoAgent(t *testing.T) {
	content := pyjson.Object{"msgtype": "m.text", "body": "hi", "m.mentions": pyjson.Object{"user_ids": []any{testAlice}}}
	event := testEvent(testAlice, content)
	r := Receiver{Self: testCoord, HumanText: true, Room: ReceiveMain}
	got, err := ValidateEvent(event, testContext(), r)
	if err != nil || got.Verdict != Ignore || got.Reason != IgnoreNotAddressed || !Unheard(event, testContext(), got) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStripReplyFallback(t *testing.T) {
	for in, want := range map[string]string{"> a\n> b\n\nc": "c", "x\n> a": "x\n> a", "> a\nc": "c", "> a": ""} {
		if got := StripReplyFallback(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestReasonOfAPlainError(t *testing.T) {
	if reasonOf(errors.New("plain")) != ReasonSchema || reasonOf(refuse(ReasonRole)) != ReasonRole {
		t.Fatal("reasonOf")
	}
}
