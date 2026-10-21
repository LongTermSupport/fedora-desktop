package fakehs

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

const teamServerName = "team-a.agent-bus.internal"

// team is a fresh server with the team room of agent-bus-protocol.md section 8: admin, one human
// (power 50), two agents (power 0), all joined, and an outsider (test_fakes.py:218-245).
type team struct {
	t        *testing.T
	s        *Server
	admin    string
	human    string
	agent    string
	other    string
	outsider string
	tok      map[string]string
	room     string
}

// answer is one decoded reply of the fake.
type answer struct {
	status int
	obj    map[string]any
	list   []any
}

func q(s string) string { return url.PathEscape(s) }

func teamLevels(human string) map[string]any {
	return map[string]any{
		"ban": 100, "events_default": 0, "invite": 100, "kick": 100, "redact": 100, "state_default": 100,
		"users_default": 0, "notifications": map[string]any{"room": 50},
		"users": map[string]any{human: 50},
		"events": map[string]any{
			"agent_bus.status": 0, "m.reaction": 50, "m.room.power_levels": 100,
			"m.room.redaction": 100, "m.room.tombstone": 150, "m.sticker": 100,
		},
	}
}

func newTeam(t *testing.T) *team {
	t.Helper()
	s := New(Options{ServerName: teamServerName, Now: fixedNow, Quirks: TuwunelQuirks()})
	tm := &team{t: t, s: s, tok: map[string]string{}}
	tm.admin = s.AddUser("admin", "", true)
	tm.human = s.AddUser("alice", "", false)
	tm.agent = s.AddUser("repo.1+ws.podman", "", false)
	tm.other = s.AddUser("repo.2+ws.podman", "", false)
	tm.outsider = s.AddUser("repo.3+ws.podman", "", false)
	for _, uid := range []string{tm.admin, tm.human, tm.agent, tm.other, tm.outsider} {
		tok, err := s.MintToken(uid)
		if err != nil {
			t.Fatal(err)
		}
		tm.tok[uid] = tok
	}
	created := tm.ok(tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{
		"preset": "private_chat", "room_version": "12", "name": "team-a", "topic": "t",
		"visibility": "private", "power_level_content_override": teamLevels(tm.human),
		"initial_state": []any{map[string]any{"type": "agent_bus.team", "state_key": "", "content": map[string]any{"v": 1}}},
	}))
	tm.room = text(t, created["room_id"])
	for _, uid := range []string{tm.human, tm.agent, tm.other} {
		tm.ok(tm.call(tm.admin, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": uid}))
		tm.ok(tm.call(uid, "POST", tm.roomPath("join"), nil, map[string]any{}))
	}
	return tm
}

func (tm *team) roomPath(rest string) string {
	return "/_matrix/client/v3/rooms/" + q(tm.room) + "/" + rest
}

func (tm *team) call(uid, method, path string, query url.Values, body any) answer {
	tm.t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			tm.t.Fatal(err)
		}
	}
	status, data := tm.s.Handle(context.Background(), method, path, query, raw, tm.tok[uid])
	got := answer{status: status}
	if err := json.Unmarshal(data, &got.obj); err != nil {
		if json.Unmarshal(data, &got.list) != nil {
			tm.t.Fatalf("%s %s: answer is not JSON: %s", method, path, data)
		}
	}
	return got
}

func (tm *team) ok(a answer) map[string]any {
	tm.t.Helper()
	if a.status != 200 {
		tm.t.Fatalf("status %d: %v", a.status, a.obj)
	}
	return a.obj
}

func (tm *team) refused(a answer, want int, errcode, text string) {
	tm.t.Helper()
	msg, isText := a.obj["error"].(string)
	if a.status != want || a.obj["errcode"] != errcode || !isText || !strings.Contains(msg, text) {
		tm.t.Fatalf("got %d %v, want %d %s %q", a.status, a.obj, want, errcode, text)
	}
}

func (tm *team) putState(uid, eventType, key string, content any) answer {
	return tm.call(uid, "PUT", tm.roomPath("state/"+eventType+"/"+q(key)), nil, content)
}

func (tm *team) send(uid, txn string, content any) answer {
	return tm.call(uid, "PUT", tm.roomPath("send/m.room.message/"+txn), nil, content)
}

func field(t *testing.T, v any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		obj, isObj := v.(map[string]any)
		if !isObj {
			t.Fatalf("no object at %q in %v", key, v)
		}
		v = obj[key]
	}
	return v
}

var listening = map[string]any{"v": 1, "state": "listening", "until": 1}

func TestPowerLevelsPerEventType(t *testing.T) {
	tm := newTeam(t)
	tm.refused(tm.putState(tm.agent, "m.room.name", "", map[string]any{"name": "x"}), 403, "M_FORBIDDEN",
		"sender does not have enough power (Int(0)) for `m.room.name` event type (100)")
	tm.refused(tm.putState(tm.human, "m.room.topic", "", map[string]any{"topic": "x"}), 403, "M_FORBIDDEN",
		"(Int(50)) for `m.room.topic` event type (100)")
	tm.ok(tm.putState(tm.admin, "m.room.topic", "", map[string]any{"topic": "x"}))
	tm.ok(tm.send(tm.agent, "t1", map[string]any{"msgtype": "m.notice", "body": "hi"}))
}

func TestStatusStateKeys(t *testing.T) {
	tm := newTeam(t)
	tm.ok(tm.putState(tm.agent, "agent_bus.status", tm.agent, listening))
	tm.refused(tm.putState(tm.agent, "agent_bus.status", tm.other, listening), 403, "M_FORBIDDEN",
		"matching another user's ID")
	tm.refused(tm.putState(tm.admin, "agent_bus.status", tm.other, listening), 403, "M_FORBIDDEN",
		"matching another user's ID")
	tm.ok(tm.putState(tm.agent, "agent_bus.status", "stray", listening))
}

func TestANonMemberCanNeitherSendNorRead(t *testing.T) {
	tm := newTeam(t)
	tm.refused(tm.send(tm.outsider, "x", map[string]any{"body": "x"}), 403, "M_FORBIDDEN", "not joined")
	tm.refused(tm.call(tm.outsider, "GET", tm.roomPath("state/m.room.create/"), nil, nil), 403,
		"M_FORBIDDEN", "permission to view this room")
	tm.refused(tm.call(tm.outsider, "POST", tm.roomPath("join"), nil, map[string]any{}), 403,
		"M_FORBIDDEN", "not invited")
}

func TestOnlyAdminInvitesAndKicks(t *testing.T) {
	tm := newTeam(t)
	tm.refused(tm.call(tm.human, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": tm.outsider}),
		403, "M_FORBIDDEN", "enough power to invite")
	tm.refused(tm.call(tm.human, "POST", tm.roomPath("kick"), nil, map[string]any{"user_id": tm.agent}),
		403, "M_FORBIDDEN", "enough power to kick")
	tm.ok(tm.call(tm.admin, "POST", tm.roomPath("kick"), nil,
		map[string]any{"user_id": tm.agent, "reason": "removed from the team"}))
	got := tm.ok(tm.call(tm.admin, "GET", tm.roomPath("state/m.room.member/"+q(tm.agent)), url.Values{"format": {"event"}}, nil))
	if field(t, got, "content", "membership") != "leave" || field(t, got, "content", "reason") != "removed from the team" ||
		got["sender"] != tm.admin {
		t.Fatalf("kick left %v", got)
	}
	tm.refused(tm.call(tm.admin, "POST", tm.roomPath("kick"), nil, map[string]any{"user_id": tm.outsider}),
		403, "M_FORBIDDEN", "not in the room")
}

func TestCreateEventIDIsTheRoomIDInV12(t *testing.T) {
	tm := newTeam(t)
	got := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("state/m.room.create/"), url.Values{"format": {"event"}}, nil))
	if got["event_id"] != "$"+tm.room[1:] || got["sender"] != tm.admin || got["room_id"] != tm.room {
		t.Fatalf("create event %v", got)
	}
	if !roomIDKey.MatchString(tm.room) {
		t.Fatalf("room ID %q is not a v12 room ID", tm.room)
	}
}

func TestTxnReuseReturnsTheSameEvent(t *testing.T) {
	tm := newTeam(t)
	first := tm.ok(tm.send(tm.agent, "same", map[string]any{"body": "one"}))
	again := tm.ok(tm.send(tm.agent, "same", map[string]any{"body": "two"}))
	if first["event_id"] != again["event_id"] {
		t.Fatalf("reused txn gave %v then %v", first, again)
	}
	other := tm.ok(tm.send(tm.other, "same", map[string]any{"body": "three"}))
	if other["event_id"] == first["event_id"] {
		t.Fatal("another user's txn shared an event")
	}
	got := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("event/"+q(text(t, first["event_id"]))), nil, nil))
	if field(t, got, "content", "body") != "one" {
		t.Fatalf("stored %v", got)
	}
	if field(t, got, "unsigned", "transaction_id") != "same" {
		t.Fatalf("the sender's own event lacks its transaction ID: %v", got)
	}
}

func TestRedactAndReceipts(t *testing.T) {
	tm := newTeam(t)
	id := text(t, tm.ok(tm.send(tm.agent, "r", map[string]any{"body": "x"}))["event_id"])
	tm.refused(tm.call(tm.human, "PUT", tm.roomPath("redact/"+q(id)+"/r1"), nil, map[string]any{}), 403,
		"M_FORBIDDEN", "`m.room.redaction` event type (100)")
	tm.ok(tm.call(tm.agent, "POST", tm.roomPath("receipt/m.read/"+q(id)), nil, map[string]any{}))
	tm.refused(tm.call(tm.agent, "POST", tm.roomPath("receipt/m.bogus/"+q(id)), nil, map[string]any{}), 400,
		"M_INVALID_PARAM", "Unknown receipt type")
	tm.refused(tm.call(tm.agent, "POST", tm.roomPath("receipt/m.read/%24nope"), nil, map[string]any{}), 404,
		"M_NOT_FOUND", "Event not found")
	want := Receipt{UserID: tm.agent, RoomID: tm.room, Type: "m.read", EventID: id}
	if got := tm.s.Receipts(); len(got) != 1 || got[0] != want {
		t.Fatalf("receipts %v, want %v", got, want)
	}
	tm.ok(tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(id)+"/r2"), nil, map[string]any{}))
	got := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("event/"+q(id)), nil, nil))
	if content, isObj := got["content"].(map[string]any); !isObj || len(content) != 0 {
		t.Fatalf("a redacted event kept its content: %v", got)
	}
}

func TestWholeRoomStateAndJoinedRooms(t *testing.T) {
	tm := newTeam(t)
	state := tm.call(tm.admin, "GET", tm.roomPath("state"), nil, nil)
	types := map[string]bool{}
	for _, e := range state.list {
		types[text(t, field(t, e, "type"))] = true
	}
	for _, want := range []string{"m.room.create", "m.room.power_levels", "m.room.join_rules", "agent_bus.team", "m.room.member", "m.room.topic", "m.room.name"} {
		if !types[want] {
			t.Errorf("the room state lacks %s", want)
		}
	}
	got := tm.ok(tm.call(tm.agent, "GET", "/_matrix/client/v3/joined_rooms", nil, nil))
	if rooms, isList := got["joined_rooms"].([]any); !isList || len(rooms) != 1 || rooms[0] != tm.room {
		t.Fatalf("joined rooms %v", got)
	}
	got = tm.ok(tm.call(tm.outsider, "GET", "/_matrix/client/v3/joined_rooms", nil, nil))
	if rooms, isList := got["joined_rooms"].([]any); !isList || len(rooms) != 0 {
		t.Fatalf("an outsider has joined rooms %v", got)
	}
}

func TestRestrictedJoinAllowsMainRoomMembers(t *testing.T) {
	tm := newTeam(t)
	created := tm.ok(tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{
		"preset": "private_chat", "room_version": "12", "name": "team-a · reports",
		"initial_state": []any{map[string]any{"type": "m.room.join_rules", "state_key": "", "content": map[string]any{
			"join_rule": "restricted", "allow": []any{map[string]any{"type": "m.room_membership", "room_id": tm.room}},
		}}},
	}))
	side := "/_matrix/client/v3/rooms/" + q(text(t, created["room_id"])) + "/join"
	tm.ok(tm.call(tm.agent, "POST", side, nil, map[string]any{}))
	tm.refused(tm.call(tm.outsider, "POST", side, nil, map[string]any{}), 403, "M_FORBIDDEN", "not invited")
}

func TestLeaveRejectsAnInvite(t *testing.T) {
	tm := newTeam(t)
	tm.ok(tm.call(tm.admin, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": tm.outsider}))
	tm.ok(tm.call(tm.outsider, "POST", tm.roomPath("leave"), nil, map[string]any{}))
	got := tm.ok(tm.call(tm.admin, "GET", tm.roomPath("state/m.room.member/"+q(tm.outsider)), nil, nil))
	if got["membership"] != "leave" {
		t.Fatalf("membership %v", got)
	}
	again := tm.call(tm.outsider, "POST", tm.roomPath("leave"), nil, map[string]any{})
	if again.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("leaving a room never joined answered %d and was not recorded as unmodelled", again.status)
	}
}

func TestUnknownRouteAndUnmodelledRequests(t *testing.T) {
	tm := newTeam(t)
	tm.refused(tm.call(tm.agent, "GET", "/_matrix/client/v3/nope", nil, nil), 404, "M_UNRECOGNIZED", "Unrecognized request")
	if err := tm.s.CheckModelled(); err != nil {
		t.Fatalf("an unknown route was recorded as unmodelled: %v", err)
	}
	got := tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{"preset": "public_chat"})
	if msg, isText := got.obj["error"].(string); got.status != 500 || !isText || !strings.HasPrefix(msg, "fake: unmodelled: ") {
		t.Fatalf("an unmodelled createRoom answered %d %v", got.status, got.obj)
	}
	if err := tm.s.CheckModelled(); err == nil || !strings.Contains(err.Error(), "createRoom") {
		t.Fatalf("CheckModelled = %v", err)
	}
}

func TestBadBodiesAndTokens(t *testing.T) {
	tm := newTeam(t)
	for _, body := range []string{"[1]", "{"} {
		status, data := tm.s.Handle(context.Background(), "PUT", tm.roomPath("send/m.room.message/x"), nil, []byte(body), tm.tok[tm.agent])
		if status != 400 || !strings.Contains(string(data), "M_NOT_JSON") {
			t.Fatalf("body %q answered %d %s", body, status, data)
		}
	}
	tm.refused(tm.call("", "GET", "/_matrix/client/v3/account/whoami", nil, nil), 401, "M_MISSING_TOKEN", "Missing access token.")
	tm.tok["nobody"] = "not-a-token"
	tm.refused(tm.call("nobody", "GET", "/_matrix/client/v3/account/whoami", nil, nil), 401, "M_UNKNOWN_TOKEN", "Unknown access token.")
	tm.refused(tm.call(tm.agent, "GET", "/_matrix/client/v3/rooms/%21nope/state/m.room.create/", nil, nil), 404, "M_NOT_FOUND", "Room not found.")
}

func TestMaxRequestSizeQuirk(t *testing.T) {
	tm := newTeam(t)
	tm.s.SetQuirks(Quirks{MaxRequestBytes: 64})
	big := map[string]any{"body": strings.Repeat("x", 100)}
	tm.refused(tm.send(tm.agent, "big", big), 413, "M_TOO_LARGE", "too large")
	tm.s.SetQuirks(Quirks{})
	tm.ok(tm.send(tm.agent, "big", big))
}

func TestInjectedFaultIsServedOnce(t *testing.T) {
	tm := newTeam(t)
	tm.s.Inject(Fault{
		Method: "GET", Path: "/account/whoami", Status: 429,
		Body: []byte(`{"errcode":"M_LIMIT_EXCEEDED","error":"slow down","retry_after_ms":2000}`),
	})
	tm.refused(tm.call(tm.agent, "GET", "/_matrix/client/v3/account/whoami", nil, nil), 429, "M_LIMIT_EXCEEDED", "slow down")
	if got := tm.ok(tm.call(tm.agent, "GET", "/_matrix/client/v3/account/whoami", nil, nil)); got["user_id"] != tm.agent {
		t.Fatalf("whoami %v", got)
	}
}

func bodyOf(t *testing.T, event any) any { t.Helper(); return field(t, event, "content", "body") }

func TestMessagesPaginates(t *testing.T) {
	tm := newTeam(t)
	for _, letter := range []string{"a", "b", "c", "d", "e"} {
		tm.ok(tm.send(tm.agent, "m"+letter, map[string]any{"body": letter}))
	}
	query := url.Values{"dir": {"b"}, "limit": {"2"}, "filter": {`{"types":["m.room.message"]}`}}
	page := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("messages"), query, nil))
	chunk, isList := page["chunk"].([]any)
	if !isList || len(chunk) != 2 || bodyOf(t, chunk[0]) != "e" {
		t.Fatalf("first backward page %v", page)
	}
	query.Set("from", text(t, page["end"]))
	page = tm.ok(tm.call(tm.agent, "GET", tm.roomPath("messages"), query, nil))
	if chunk, isList = page["chunk"].([]any); !isList || bodyOf(t, chunk[0]) != "c" {
		t.Fatalf("second backward page %v", page)
	}
	tm.refused(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{"dir": {"x"}}, nil), 400, "M_INVALID_PARAM", "dir")
	tm.refused(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{"dir": {"f"}, "from": {"abc"}}, nil), 400, "M_INVALID_PARAM", "pagination token")
	tm.refused(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{"dir": {"f"}, "limit": {"many"}}, nil), 400, "M_INVALID_PARAM", "limit")
}
