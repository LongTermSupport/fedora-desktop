package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	human = "@human:alpha.agent-bus.internal"
	agent = "@repo.1+host.podman:alpha.agent-bus.internal"
	// at1 is the time of event 1, a second after the fixed clock.
	at1 = "2026-11-24T10:11:13.345Z"
)

func render(t *testing.T, raw json.RawMessage) (string, bool) {
	t.Helper()
	return renderEvent(raw)
}

// E15: each event kind's lines.
func TestE15EventKinds(t *testing.T) {
	cases := map[string]struct {
		event json.RawMessage
		want  string
	}{
		"ping": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.notice", "body": "[agent-bus] review abc -> x",
			"agent_bus.ping": map[string]any{"v": 1, "verb": "review", "to": []string{"@a:x", "@b:x"}, "ref": "abc123", "branch": "main", "re": eventID(9)},
		}), at1 + " " + agent + " PING review abc123 on main -> @a:x,@b:x re " + eventID(9) + "\n"},
		"ping bare": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.notice", "body": "b", "agent_bus.ping": map[string]any{"v": 1, "verb": "sync", "to": []string{"@a:x"}},
		}), at1 + " " + agent + " PING sync - -> @a:x\n"},
		"text": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.notice", "body": "[agent-bus] text -> @a:x\nhello", "agent_bus.text": map[string]any{"v": 1, "to": []string{"@a:x"}, "text": "hello\nthere"},
		}), at1 + " " + agent + " TEXT -> @a:x\n    hello\n    there\n"},
		"report": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.notice", "body": "done it", "format": "org.matrix.custom.html", "formatted_body": "<b>done</b>", "agent_bus.report": map[string]any{"v": 1},
		}), at1 + " " + agent + " REPORT\n    done it\n"},
		"question": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.text", "body": "Q3 ❓ which way?", "agent_bus.question": map[string]any{"v": 1, "n": 3, "asked_by": []string{}, "re": []string{}},
		}), at1 + " " + agent + " QUESTION Q3\n    Q3 ❓ which way?\n"},
		"message": {
			msgEvent(t, 1, human, map[string]any{"msgtype": "m.text", "body": "hi <b>all</b>"}),
			at1 + " " + human + " MESSAGE\n    hi <b>all</b>\n",
		},
		"reply fallback": {msgEvent(t, 1, human, map[string]any{
			"msgtype": "m.text", "body": "> <@a:x> earlier\n> more\n\nmy answer",
			"m.relates_to": map[string]any{"m.in_reply_to": map[string]any{"event_id": eventID(2)}},
		}), at1 + " " + human + " MESSAGE\n    my answer\n"},
		"edit": {msgEvent(t, 1, human, map[string]any{
			"msgtype": "m.text", "body": "* fixed", "m.new_content": map[string]any{"msgtype": "m.text", "body": "fixed"},
			"m.relates_to": map[string]any{"rel_type": "m.replace", "event_id": eventID(2)},
		}), at1 + " " + human + " EDIT of " + eventID(2) + "\n    fixed\n"},
		"edit without new content": {msgEvent(t, 1, human, map[string]any{
			"msgtype": "m.text", "body": "* fixed", "m.relates_to": map[string]any{"rel_type": "m.replace", "event_id": eventID(2)},
		}), at1 + " " + human + " EDIT of " + eventID(2) + "\n    * fixed\n"},
		"thread": {msgEvent(t, 1, human, map[string]any{
			"msgtype": "m.text", "body": "in it", "m.relates_to": map[string]any{"rel_type": "m.thread", "event_id": eventID(5), "is_falling_back": true},
		}), at1 + " " + human + " MESSAGE in thread " + eventID(5) + "\n    in it\n"},
		"thread text": {msgEvent(t, 1, agent, map[string]any{
			"msgtype": "m.notice", "body": "x", "agent_bus.text": map[string]any{"v": 1, "to": []string{"@a:x"}, "text": "t"},
			"m.relates_to": map[string]any{"rel_type": "m.thread", "event_id": eventID(5)},
		}), at1 + " " + agent + " TEXT -> @a:x in thread " + eventID(5) + "\n    t\n"},
		"redacted": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.message", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"content": map[string]any{}, "unsigned": map[string]any{"redacted_because": map[string]any{"type": "m.room.redaction"}},
		}), at1 + " " + human + " [redacted] m.room.message\n"},
		"redaction": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.redaction", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"redacts": eventID(3), "content": map[string]any{},
		}), at1 + " " + human + " REDACTION of " + eventID(3) + "\n"},
		"redaction v11": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.redaction", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"content": map[string]any{"redacts": eventID(3)},
		}), at1 + " " + human + " REDACTION of " + eventID(3) + "\n"},
		"reaction": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.reaction", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"content": map[string]any{"m.relates_to": map[string]any{"rel_type": "m.annotation", "event_id": eventID(4), "key": "👍"}},
		}), at1 + " " + human + " REACTION 👍 to " + eventID(4) + "\n"},
		"state": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.member", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"state_key": human, "content": map[string]any{"membership": "join"},
		}), at1 + " " + human + " STATE m.room.member " + human + "\n"},
		"state empty key": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.create", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"state_key": "", "content": map[string]any{},
		}), at1 + " " + human + ` STATE m.room.create ""` + "\n"},
		"control characters": {
			msgEvent(t, 1, human, map[string]any{"msgtype": "m.text", "body": "a\x1b[31mred\r\nb\tc\x00"}),
			at1 + " " + human + " MESSAGE\n    a\\u001b[31mred\\u000d\n    b\\u0009c\\u0000\n",
		},
		"control in sender": {
			msgEvent(t, 1, "@a\nb:x", map[string]any{"msgtype": "m.text", "body": "x"}),
			at1 + " @a\\u000ab:x MESSAGE\n    x\n",
		},
		"unknown type": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.encrypted", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000, "content": map[string]any{},
		}), at1 + " " + human + " UNREADABLE " + eventID(1) + " m.room.encrypted\n"},
		"no body": {
			msgEvent(t, 1, human, map[string]any{"msgtype": "m.text"}),
			at1 + " " + human + " UNREADABLE " + eventID(1) + " m.room.message\n",
		},
		"ping not an object": {
			msgEvent(t, 1, agent, map[string]any{"msgtype": "m.notice", "body": "b", "agent_bus.ping": "x"}),
			at1 + " " + agent + " UNREADABLE " + eventID(1) + " m.room.message\n",
		},
		"text with no to": {
			msgEvent(t, 1, agent, map[string]any{"msgtype": "m.notice", "body": "b", "agent_bus.text": map[string]any{"text": "t"}}),
			at1 + " " + agent + " UNREADABLE " + eventID(1) + " m.room.message\n",
		},
		"question with no n": {
			msgEvent(t, 1, agent, map[string]any{"msgtype": "m.text", "body": "b", "agent_bus.question": map[string]any{}}),
			at1 + " " + agent + " UNREADABLE " + eventID(1) + " m.room.message\n",
		},
		"reaction with no key": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.reaction", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000,
			"content": map[string]any{"m.relates_to": map[string]any{"event_id": eventID(4)}},
		}), at1 + " " + human + " UNREADABLE " + eventID(1) + " m.reaction\n"},
		"edit with no target": {
			msgEvent(t, 1, human, map[string]any{"msgtype": "m.text", "body": "b", "m.relates_to": map[string]any{"rel_type": "m.replace"}}),
			at1 + " " + human + " UNREADABLE " + eventID(1) + " m.room.message\n",
		},
		"redaction of nothing": {ev(t, map[string]any{
			"event_id": eventID(1), "type": "m.room.redaction", "sender": human, "origin_server_ts": fixed.UnixMilli() + 1000, "content": map[string]any{},
		}), at1 + " " + human + " UNREADABLE " + eventID(1) + " m.room.redaction\n"},
		"bad timestamp":   {json.RawMessage(`{"event_id":"$x","type":"m.room.message","sender":"@a:x","origin_server_ts":"soon","content":{"body":"b"}}`), "- @a:x UNREADABLE $x m.room.message\n"},
		"no fields":       {json.RawMessage(`{}`), "- - UNREADABLE - -\n"},
		"content a list":  {json.RawMessage(`{"event_id":"$x","type":"m.room.message","sender":"@a:x","origin_server_ts":1,"content":[]}`), "1970-01-01T00:00:00.001Z @a:x UNREADABLE $x m.room.message\n"},
		"unsigned a list": {json.RawMessage(`{"event_id":"$x","type":"m.room.message","sender":"@a:x","origin_server_ts":1,"content":{"body":"b"},"unsigned":[]}`), "1970-01-01T00:00:00.001Z @a:x UNREADABLE $x m.room.message\n"},
		"quote not a reply": {
			msgEvent(t, 1, human, map[string]any{"msgtype": "m.text", "body": "> quoted\nmine"}),
			at1 + " " + human + " MESSAGE\n    > quoted\n    mine\n",
		},
	}
	for name, c := range cases {
		got, status := render(t, c.event)
		if got != c.want || status {
			t.Errorf("%s:\n got %q\nwant %q (status %v)", name, got, c.want, status)
		}
	}
}

// E15: agent_bus.status is counted, not listed.
func TestE15StatusCounted(t *testing.T) {
	status := ev(t, map[string]any{
		"event_id": eventID(1), "type": "agent_bus.status", "sender": agent, "origin_server_ts": fixed.UnixMilli(),
		"state_key": agent, "content": map[string]any{"v": 1, "state": "listening", "until": 1},
	})
	if got, isStatus := render(t, status); !isStatus || got != "" {
		t.Fatalf("%q %v", got, isStatus)
	}
	doc := renderHistory("alpha", alphaServer, fixed, []roomRecord{
		{purpose: "main", id: roomID('m'), events: []json.RawMessage{status, textEvents(t, 2)[1], status}},
		{purpose: "questions", id: roomID('q')},
	})
	want := "# v1 team alpha\n\nServer " + alphaServer + ", exported 2026-11-24T10:11:12.345Z.\n\n" +
		"## main " + roomID('m') + "\n\n" +
		at1 + " " + human + " MESSAGE\n    line 1\n\n" +
		"agent_bus.status: 2 events, counted and not listed.\n\n" +
		"## questions " + roomID('q') + "\n\n(no events)\n"
	if doc != want {
		t.Errorf("got\n%s\nwant\n%s", doc, want)
	}
	if strings.Contains(doc, "listening") {
		t.Error("a status event was listed")
	}
}
