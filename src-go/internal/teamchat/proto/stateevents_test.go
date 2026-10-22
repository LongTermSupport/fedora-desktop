package proto

import (
	"encoding/json"
	"reflect"
	"testing"
)

// StateEvents reads a room's state as the trust checks take it; an event that does not read is
// left out, so the checks find what is missing.
func TestStateEvents(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"type":"m.room.create","state_key":"","sender":"@admin:s","event_id":"$a","content":{"room_version":"12"}}`),
		json.RawMessage(`not json`),
		json.RawMessage(`[]`),
		json.RawMessage(`{"type":"m.room.member","state_key":"@x:s","sender":"@x:s","content":{}}`),
		json.RawMessage(`{"type":"m.room.member","state_key":"@x:s","sender":"@x:s","event_id":"$b","content":"no"}`),
		json.RawMessage(`{"type":"m.room.member","state_key":1,"sender":"@x:s","event_id":"$b","content":{}}`),
		json.RawMessage(`{"type":"m.room.member","state_key":"@x:s","sender":"@x:s","event_id":"$c","content":{"membership":"join"}}`),
	}
	got := StateEvents(raw)
	if len(got) != 2 {
		t.Fatalf("read %d events, want 2: %+v", len(got), got)
	}
	want := StateEvent{Type: CreateType, StateKey: "", Sender: "@admin:s", EventID: "$a", Content: Obj(M("room_version", Str("12")))}
	if got[0].Type != want.Type || got[0].Sender != want.Sender || got[0].EventID != want.EventID || !got[0].Content.Equal(want.Content) {
		t.Errorf("first = %+v, want %+v", got[0], want)
	}
	if got[1].EventID != "$c" || got[1].StateKey != "@x:s" {
		t.Errorf("second = %+v", got[1])
	}
	if empty := StateEvents(nil); empty == nil || len(empty) != 0 {
		t.Errorf("StateEvents(nil) = %#v, want an empty list", empty)
	}
	if !reflect.DeepEqual(StateEvents(raw[1:3]), []StateEvent{}) {
		t.Error("unreadable events are not left out")
	}
}
