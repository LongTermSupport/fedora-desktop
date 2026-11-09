package proto

// RedactedSlot is the event ID of a message that the admin redacted and an agent account or the
// admin sent (SPEC-n3f QX7): a number a question may have held. The judge drops a redacted event
// as `schema` before the index sees it (A 7.6 rule 02), so the engine asks this of the raw event
// and gives the ID to QuestionIndex.Slot. A redaction by anyone but the admin, a human's event
// and anything that is not a message are not slots.
func RedactedSlot(raw Value, server string) (string, bool) {
	id := stringMember(raw, "event_id")
	if stringMember(raw, "type") != MessageType || !ValidEventID(id) {
		return "", false
	}
	if _, state := raw.Get("state_key"); state {
		return "", false
	}
	if class := ClassifyUserID(stringMember(raw, "sender"), server); class != IDAgent && class != IDAdmin {
		return "", false
	}
	// `unsigned.redacted_because` is the redaction event: an object, else it has no sender.
	because := memberOrNull(memberOrNull(raw, "unsigned"), "redacted_because")
	if stringMember(because, "sender") != AdminID(server) {
		return "", false
	}
	return id, true
}

// memberOrNull is the value of an object's key, null when it has none.
func memberOrNull(v Value, key string) Value {
	if m, found := v.Get(key); found {
		return m
	}
	return Null()
}
