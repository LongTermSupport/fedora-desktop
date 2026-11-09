package proto

import (
	"strings"
	"testing"
)

// redactedRaw is a message of sender as a server returns it after a redaction: its content
// stripped and unsigned.redacted_because the redaction event, by redactor.
func redactedRaw(id, sender, redactor string) string {
	return `{"type":"m.room.message","event_id":"` + id + `","sender":"` + sender + `","origin_server_ts":5,"content":{},` +
		`"unsigned":{"redacted_because":{"type":"m.room.redaction","sender":"` + redactor + `","content":{"reason":"secret"}}}}`
}

func TestRedactedSlotIsWhatTheJudgeDropsAsSchema(t *testing.T) {
	judge := newQuestionRoom(t, 0).judge
	for name, sender := range map[string]string{"an agent": coordID, "the admin": adminID} {
		raw := mustParse(t, redactedRaw(qid('r'), sender, adminID))
		if v := judge.Judge(raw); v.Outcome != Drop || v.Reason != ReasonSchema {
			t.Errorf("%s: the judge says %v %q, want Drop schema: a redacted event never reaches the index", name, v.Outcome, v.Reason)
		}
		if id, ok := RedactedSlot(raw, testServer); !ok || id != qid('r') {
			t.Errorf("%s: RedactedSlot = %q, %v, want the event's id", name, id, ok)
		}
	}
}

func TestRedactedSlotRefuses(t *testing.T) {
	id := qid('r')
	whole := redactedRaw(id, coordID, adminID)
	cases := map[string]string{
		"a human's event":                       redactedRaw(id, aliceID, adminID),
		"a sender of another server":            redactedRaw(id, UserID("myrepo.1+desk.podman", "other.example"), adminID),
		"a sender that is no user id":           redactedRaw(id, "nobody", adminID),
		"a redaction by an agent":               redactedRaw(id, coordID, workerID),
		"a redaction by another server's admin": redactedRaw(id, coordID, AdminID("other.example")),
		"a redaction by no one":                 redactedRaw(id, coordID, ""),
		"an event id that is malformed":         redactedRaw("$short", coordID, adminID),
		"an event of another type":              strings.Replace(whole, `"m.room.message"`, `"m.room.member"`, 1),
		"a state event":                         strings.Replace(whole, `"content":{}`, `"state_key":"","content":{}`, 1),
		"a redacted_because that is no object":  strings.Replace(whole, `"redacted_because":{`, `"redacted_because":true,"other":{`, 1),
		"an unsigned that is no object":         `{"type":"m.room.message","event_id":"` + id + `","sender":"` + coordID + `","content":{},"unsigned":7}`,
		"an event not redacted":                 `{"type":"m.room.message","event_id":"` + id + `","sender":"` + coordID + `","content":{},"unsigned":{}}`,
		"an event with no unsigned":             `{"type":"m.room.message","event_id":"` + id + `","sender":"` + coordID + `","content":{}}`,
		"no event id":                           strings.Replace(whole, `"event_id":"`+id+`",`, ``, 1),
		"a value that is not an event":          `[]`,
	}
	for name, doc := range cases {
		if got, ok := RedactedSlot(mustParse(t, doc), testServer); ok {
			t.Errorf("%s: RedactedSlot = %q, true", name, got)
		}
	}
}

func TestRedactedSlotHoldsTheNumberItsEventTook(t *testing.T) {
	ix := NewQuestionIndex(testServer, 0)
	askAgent(ix, 1)
	raw := mustParse(t, redactedRaw(eid(2), coordID, adminID))
	id, ok := RedactedSlot(raw, testServer)
	if !ok || !ix.Slot(id) || ix.Next() != 3 {
		t.Fatalf("a redacted Q2: slot %q %v, Next() = %d, want 3", id, ok, ix.Next())
	}
	if again, found := RedactedSlot(raw, testServer); !found || ix.Slot(again) {
		t.Errorf("the same redacted event made a second slot (found %v)", found)
	}
	if !askAgent(ix, 3) {
		t.Fatal("Q3 was not counted")
	}
	if q := mustQ(t, ix, 2); !q.Closed || !q.Redacted() {
		t.Errorf("Q2 = %+v, want a number held by a redacted event", q)
	}
}
