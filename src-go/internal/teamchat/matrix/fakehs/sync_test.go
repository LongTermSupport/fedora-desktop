package fakehs

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func syncFilter(t *testing.T, rooms []string, limit int, lazy bool) string {
	t.Helper()
	room := map[string]any{
		"state":    map[string]any{"types": []string{"m.room.create", "m.room.member", "agent_bus.team"}, "lazy_load_members": lazy},
		"timeline": map[string]any{"types": []string{"m.room.message", "m.room.member"}, "limit": limit},
	}
	if rooms != nil {
		room["rooms"] = rooms
	}
	raw, err := json.Marshal(map[string]any{"room": room})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (tm *team) sync(uid, since, filter string, timeout int) map[string]any {
	tm.t.Helper()
	query := url.Values{"timeout": {strconv.Itoa(timeout)}}
	if filter != "" {
		query.Set("filter", filter)
	}
	if since != "" {
		query.Set("since", since)
	}
	return tm.ok(tm.call(uid, "GET", "/_matrix/client/v3/sync", query, nil))
}

func joinedRoom(t *testing.T, answer map[string]any, room string) map[string]any {
	t.Helper()
	got, ok := field(t, answer, "rooms", "join", room).(map[string]any)
	if !ok {
		t.Fatalf("no joined room %s in %v", room, answer)
	}
	return got
}

func list(t *testing.T, v any) []any {
	t.Helper()
	got, ok := v.([]any)
	if !ok {
		t.Fatalf("not a list: %v", v)
	}
	return got
}

func TestHeldSyncWakesWhenAnEventLands(t *testing.T) {
	tm := newTeam(t)
	filter := syncFilter(t, nil, 10, false)
	since := text(t, tm.sync(tm.agent, "", filter, 0)["next_batch"])
	done := make(chan map[string]any, 1)
	go func() { done <- tm.sync(tm.agent, since, filter, 30000) }()
	tm.ok(tm.send(tm.other, "wake", map[string]any{"body": "wake"}))
	select {
	case got := <-done:
		events := list(t, field(t, joinedRoom(t, got, tm.room), "timeline", "events"))
		if len(events) != 1 || bodyOf(t, events[0]) != "wake" {
			t.Fatalf("woke with %v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the held sync never woke")
	}
}

func TestHeldSyncEndsEmptyAtItsTimeout(t *testing.T) {
	tm := newTeam(t)
	filter := syncFilter(t, nil, 10, false)
	since := text(t, tm.sync(tm.agent, "", filter, 0)["next_batch"])
	got := tm.sync(tm.agent, since, filter, 5)
	if got["rooms"] != nil || got["next_batch"] != since {
		t.Fatalf("an idle held sync gave %v", got)
	}
}

type reply struct {
	status int
	body   []byte
}

func TestHeldSyncEndsWhenTheCallerGoes(t *testing.T) {
	tm := newTeam(t)
	filter := syncFilter(t, nil, 10, false)
	since := text(t, tm.sync(tm.agent, "", filter, 0)["next_batch"])
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan reply, 1)
	go func() {
		status, body := tm.s.Handle(ctx, "GET", "/_matrix/client/v3/sync",
			url.Values{"since": {since}, "timeout": {"30000"}, "filter": {filter}}, nil, tm.tok[tm.agent])
		done <- reply{status, body}
	}()
	cancel()
	select {
	case got := <-done:
		if got.status != 200 {
			t.Fatalf("an abandoned sync answered %d %s", got.status, got.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the held sync outlived its caller")
	}
}

func TestLimitedTimelineLeavesAGapForMessages(t *testing.T) {
	tm := newTeam(t)
	filter := syncFilter(t, []string{tm.room}, 3, false)
	since := text(t, tm.sync(tm.agent, "", filter, 0)["next_batch"])
	for i := range 6 {
		tm.ok(tm.send(tm.other, "burst"+strconv.Itoa(i), map[string]any{"body": strconv.Itoa(i)}))
	}
	timeline := field(t, joinedRoom(t, tm.sync(tm.agent, since, filter, 0), tm.room), "timeline")
	events := list(t, field(t, timeline, "events"))
	if field(t, timeline, "limited") != true || len(events) != 3 || bodyOf(t, events[0]) != "3" {
		t.Fatalf("timeline %v", timeline)
	}
	gap := tm.ok(tm.call(tm.agent, "GET", tm.roomPath("messages"), url.Values{
		"dir": {"f"}, "from": {since}, "to": {text(t, field(t, timeline, "prev_batch"))},
		"filter": {`{"types":["m.room.message"]}`}, "limit": {"100"},
	}, nil))
	if chunk := list(t, gap["chunk"]); len(chunk) != 3 || bodyOf(t, chunk[0]) != "0" || bodyOf(t, chunk[2]) != "2" {
		t.Fatalf("the gap %v", gap)
	}
}

func TestAnInviteeSeesStrippedState(t *testing.T) {
	tm := newTeam(t)
	tm.ok(tm.call(tm.admin, "POST", tm.roomPath("invite"), nil, map[string]any{"user_id": tm.outsider}))
	got := tm.sync(tm.outsider, "", syncFilter(t, nil, 0, true), 0)
	events := list(t, field(t, got, "rooms", "invite", tm.room, "invite_state", "events"))
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, text(t, field(t, e, "type")))
	}
	want := []string{"m.room.create", "m.room.join_rules", "m.room.name", "m.room.topic", "m.room.member", "m.room.member"}
	if len(types) != len(want) {
		t.Fatalf("invite state %v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("invite state %v, want %v", types, want)
		}
	}
	if field(t, events[0], "origin_server_ts") == nil || field(t, events[1], "event_id") != nil {
		t.Fatalf("stripped events %v", events)
	}
	again := tm.sync(tm.outsider, text(t, got["next_batch"]), syncFilter(t, nil, 0, true), 0)
	if again["rooms"] != nil {
		t.Fatalf("an old invite came again: %v", again)
	}
}

func TestLazyMembersAreTheTimelinesSenders(t *testing.T) {
	tm := newTeam(t)
	tm.ok(tm.send(tm.other, "only", map[string]any{"body": "x"}))
	filter := `{"room":{"timeline":{"types":["m.room.message"],"limit":1},"state":{"types":["m.room.member"],"lazy_load_members":true}}}`
	state := list(t, field(t, joinedRoom(t, tm.sync(tm.agent, "", filter, 0), tm.room), "state", "events"))
	if len(state) != 1 || field(t, state[0], "state_key") != tm.other {
		t.Fatalf("lazy members %v", state)
	}
}

func TestSyncRefusals(t *testing.T) {
	tm := newTeam(t)
	sync := "/_matrix/client/v3/sync"
	tm.refused(tm.call(tm.agent, "GET", sync, url.Values{"since": {"x"}}, nil), 400, "M_INVALID_PARAM", "pagination token")
	tm.refused(tm.call(tm.agent, "GET", sync, url.Values{"timeout": {"soon"}}, nil), 400, "M_INVALID_PARAM", "timeout")
	tm.refused(tm.call(tm.agent, "GET", sync, url.Values{"filter": {`{"room":`}}, nil), 400, "M_INVALID_PARAM", "filter")
	got := tm.call(tm.agent, "GET", sync, url.Values{"filter": {"7"}}, nil)
	if got.status != 500 || tm.s.CheckModelled() == nil {
		t.Fatalf("a stored filter ID answered %d %v", got.status, got.obj)
	}
}

func TestAFilterNamesItsRooms(t *testing.T) {
	tm := newTeam(t)
	got := tm.sync(tm.agent, "", syncFilter(t, []string{"!other"}, 10, false), 0)
	if got["rooms"] != nil {
		t.Fatalf("a room outside the filter came: %v", got)
	}
	whole := tm.sync(tm.agent, "", "", 0)
	summary := field(t, joinedRoom(t, whole, tm.room), "summary")
	if field(t, summary, "m.joined_member_count") != 4.0 || len(list(t, field(t, summary, "m.heroes"))) != 3 {
		t.Fatalf("summary %v", summary)
	}
}
