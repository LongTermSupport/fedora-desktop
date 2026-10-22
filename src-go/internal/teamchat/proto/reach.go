package proto

import "slices"

// HumanReader is an agent a human's message can reach: a non-parked coordinator or worker that
// reads human text. A mention of any other account (a parked member, the probe, a worker with
// human_text false) is delivered by no member.
func (r Roster) HumanReader(id string) bool { return r.AddressableAgent(id) && r.HumanText(id) }

// ReachedNoAgent is the "reached no agent" predicate of UH1 over a decoded human message (gap
// 11): no HumanReader in its mentions, no `@room`, no reply to or thread rooted at an event a
// HumanReader sent, and no edit of a message whose Related.AddressedToRosterAgent is set. A
// related event the lookup does not hold reaches no agent (n3-D20's undercount). It has no
// self: a message addressed to agent X has reached an agent at every member.
func ReachedNoAgent(h Human, r Roster, l Lookup) bool {
	if h.MentionRoom || slices.ContainsFunc(h.Mentions, r.HumanReader) {
		return false
	}
	if relatedReaches(l, h.ReplyTo, func(rel Related) bool { return r.HumanReader(rel.Sender) }) ||
		relatedReaches(l, h.ThreadRoot, func(rel Related) bool { return r.HumanReader(rel.Sender) }) {
		return false
	}
	return !relatedReaches(l, h.Revises, func(rel Related) bool { return rel.AddressedToRosterAgent })
}

// relatedReaches is whether the event id names is held by the lookup and reaches by its rule;
// no id reaches nothing.
func relatedReaches(l Lookup, id string, reaches func(Related) bool) bool {
	if id == "" {
		return false
	}
	rel, ok := l.Event(id)
	return ok && reaches(rel)
}

// RelationIDs are the related event ids a human's message reads for its addressing, in the
// order reply, thread root, edit original, each once: what SY4a resolves before Judge runs. It
// is nil for any other event: one that does not decode (rule 02), one not from a listed human,
// a carried item, or a message DecodeHuman refuses, whose addressing is never computed.
func RelationIDs(raw Value, r Roster) []string {
	ev, ok := decodeEvent(raw)
	if stringMember(raw, "type") != MessageType || !ok || !r.IsHuman(ev.Sender) {
		return nil
	}
	if _, carried := ev.Content.Get(KeyCarried); carried {
		return nil
	}
	h, reason := DecodeHuman(ev.Content)
	if reason != "" {
		return nil
	}
	var ids []string
	for _, id := range []string{h.ReplyTo, h.ThreadRoot, h.Revises} {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}
