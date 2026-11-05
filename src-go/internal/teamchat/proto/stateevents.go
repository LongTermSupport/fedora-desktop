package proto

import "encoding/json"

// StateEvents is a room's state, as the homeserver hands it, in the form the trust checks
// (VerifyMain, VerifySide) take: an event that does not read (not a JSON object, a type, state
// key, sender or event id that is not a string, a content that is not an object) is left out,
// so the checks find what is missing.
func StateEvents(raw []json.RawMessage) []StateEvent {
	out := make([]StateEvent, 0, len(raw))
	for _, r := range raw {
		v, err := Parse(r)
		if err != nil || v.Kind() != KindObject {
			continue
		}
		strs := map[string]string{}
		for _, name := range []string{"type", "state_key", "sender", "event_id"} {
			if f, ok := v.Get(name); ok && f.Kind() == KindString {
				strs[name] = f.Str()
			}
		}
		content, ok := v.Get("content")
		if !ok || content.Kind() != KindObject || len(strs) != 4 {
			continue
		}
		out = append(out, StateEvent{Type: strs["type"], StateKey: strs["state_key"], Sender: strs["sender"], EventID: strs["event_id"], Content: content})
	}
	return out
}
