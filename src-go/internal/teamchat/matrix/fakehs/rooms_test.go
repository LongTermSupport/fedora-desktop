package fakehs

import (
	"net/url"
	"strings"
	"testing"
)

func text(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("not a string: %v", v)
	}
	return s
}

func TestCreateRoomInvitesAndRefusesUnknownInvitees(t *testing.T) {
	tm := newTeam(t)
	created := tm.ok(tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{
		"invite": []any{tm.outsider},
	}))
	side := "/_matrix/client/v3/rooms/" + q(text(t, created["room_id"])) + "/join"
	tm.ok(tm.call(tm.outsider, "POST", side, nil, map[string]any{}))
	tm.refused(tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{
		"invite": []any{"@ghost:" + teamServerName},
	}), 404, "M_NOT_FOUND", "User not found.")
	tm.refused(tm.call(tm.admin, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": tm.agent}),
		403, "M_FORBIDDEN", "cannot invite user that is joined or banned")
	tm.refused(tm.call(tm.outsider, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": tm.agent}),
		403, "M_FORBIDDEN", "sender is not joined to the room")
}

func TestCreateRoomBeyondTheModelIsUnmodelled(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"unknown key":   {"creation_content": map[string]any{}},
		"version":       {"room_version": "11"},
		"bad state":     {"initial_state": []any{"x"}},
		"bad invite":    {"invite": "x"},
		"state no type": {"initial_state": []any{map[string]any{"content": map[string]any{}}}},
		"bad override":  {"power_level_content_override": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			tm := newTeam(t)
			got := tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, body)
			if got.status != 500 || tm.s.CheckModelled() == nil {
				t.Fatalf("answered %d %v", got.status, got.obj)
			}
		})
	}
}

func TestEventsCarryTheirOwnTransactionIDOnly(t *testing.T) {
	tm := newTeam(t)
	id := text(t, tm.ok(tm.send(tm.agent, "own", map[string]any{"body": "x"}))["event_id"])
	got := tm.ok(tm.call(tm.other, "GET", tm.roomPath("event/"+q(id)), nil, nil))
	unsigned, isObj := got["unsigned"].(map[string]any)
	if !isObj || unsigned["transaction_id"] != nil || got["room_id"] != tm.room {
		t.Fatalf("another member sees %v", got)
	}
	tm.refused(tm.call(tm.agent, "GET", tm.roomPath("event/%24nope"), nil, nil), 404, "M_NOT_FOUND", "Event not found.")
	tm.refused(tm.call(tm.outsider, "GET", tm.roomPath("state"), nil, nil), 403, "M_FORBIDDEN", "permission to view")
	tm.refused(tm.call(tm.agent, "GET", tm.roomPath("state/teamchat.nothing/"), nil, nil), 404, "M_NOT_FOUND", "Event not found.")
}

func TestRedactingAStateEventIsUnmodelled(t *testing.T) {
	tm := newTeam(t)
	id := text(t, tm.ok(tm.putState(tm.admin, "m.room.topic", "", map[string]any{"topic": "y"}))["event_id"])
	got := tm.call(tm.admin, "PUT", tm.roomPath("redact/"+q(id)+"/r"), nil, map[string]any{})
	if got.status != 500 || !strings.Contains(text(t, got.obj["error"]), "redacting a state event") {
		t.Fatalf("answered %d %v", got.status, got.obj)
	}
	tm.refused(tm.call(tm.admin, "PUT", tm.roomPath("redact/%24nope/r2"), nil, map[string]any{}), 404, "M_NOT_FOUND", "Event not found.")
}

func TestJoinTwiceAddsNoEvent(t *testing.T) {
	tm := newTeam(t)
	before := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("state/m.room.member/"+q(tm.agent)), url.Values{"format": {"event"}}, nil))
	tm.ok(tm.call(tm.agent, "POST", tm.roomPath("join"), nil, map[string]any{}))
	after := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("state/m.room.member/"+q(tm.agent)), url.Values{"format": {"event"}}, nil))
	if before["event_id"] != after["event_id"] {
		t.Fatalf("a second join replaced %v with %v", before, after)
	}
	tm.refused(tm.call(tm.agent, "POST", "/_matrix/client/v3/rooms/%21nope/join", nil, map[string]any{}), 404, "M_NOT_FOUND", "Room not found.")
}

func TestABannedMemberCannotJoinARestrictedRoom(t *testing.T) {
	tm := newTeam(t)
	created := tm.ok(tm.call(tm.admin, "POST", "/_matrix/client/v3/createRoom", nil, map[string]any{
		"initial_state": []any{map[string]any{"type": "m.room.join_rules", "content": map[string]any{
			"join_rule": "restricted", "allow": []any{"junk", map[string]any{"type": "m.room_membership", "room_id": "!gone"}},
		}}},
	}))
	side := "/_matrix/client/v3/rooms/" + q(text(t, created["room_id"]))
	tm.refused(tm.call(tm.agent, "POST", side+"/join", nil, map[string]any{}), 403, "M_FORBIDDEN", "not invited")
}

func TestPowerLevelsThatAreNotNumbersAreUnmodelled(t *testing.T) {
	tm := newTeam(t)
	levels := teamLevels(tm.human)
	levels["users_default"] = "lots"
	tm.ok(tm.putState(tm.admin, "m.room.power_levels", "", levels))
	got := tm.send(tm.agent, "x", map[string]any{"body": "x"})
	if got.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("answered %d %v", got.status, got.obj)
	}
}
