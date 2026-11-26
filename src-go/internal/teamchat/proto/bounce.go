package proto

import "slices"

// Bounce is whether this member owes the sender of the event v judges a bounced receipt (Plan
// 00267 DESIGN A, C2), and its reason. Every member judges every event, and an agent event's
// content is checked before its addressing, so a bounce is owed by the members the event
// addressed only, or there would be one per member:
//
//   - a drop of an agent's event, by each addressee: its decoded `to`, else the `to` of a known
//     typed key read from the raw content; with no addressee readable, by the coordinator;
//   - a drop of a human's post, by each member it mentions (or every member, for `@room`), and
//     a stale one by its addressee; a `type` or `empty` ignore likewise;
//   - a human's post that reached no agent at all, by the coordinator, as `unheard`;
//   - a drop in the questions room, by the coordinator, the one member that judges it.
//
// Never to an outsider, the admin (software, whose faults are logged) or this member itself,
// never in the reports room, and never for a receipt, whether or not it decodes (the loop
// guard, ReceiptOwed).
func (j Judge) Bounce(v Verdict) (string, bool) {
	if reason, owed := j.bounce(v); owed {
		return reason, true
	}
	return "", false
}

// bounce is Bounce's rules; its reason means nothing when it owes none.
func (j Judge) bounce(v Verdict) (string, bool) {
	sender := v.Event.Sender
	switch class := j.Roster.Sender(sender, j.Server); {
	case !ValidEventID(v.Event.ID) || sender == j.Self || class == SenderNone || class == SenderAdmin:
		return "", false
	case j.Purpose == PurposeReports || !ReceiptOwed(v.Event.Content):
		return "", false
	case v.Outcome == Drop && j.Purpose == PurposeQuestions:
		return v.Reason, j.coordinates()
	case v.Outcome == Drop && class == SenderHuman:
		return v.Reason, v.Reason == ReasonStale || j.mentioned(v.Event.Content)
	case v.Outcome == Drop:
		return v.Reason, j.agentBouncer(v)
	case v.Outcome != Ignore || class != SenderHuman:
		return "", false
	case v.Reason == IgnoreType || v.Reason == IgnoreEmpty:
		return v.Reason, j.mentioned(v.Event.Content)
	case v.Reason == IgnoreNotAddressed && j.Purpose == PurposeMain && ReachedNoAgent(v.Human, j.Roster, j.Lookup):
		return ReasonUnheard, j.coordinates()
	}
	return "", false
}

// coordinates is whether this member is the team's coordinator.
func (j Judge) coordinates() bool {
	return j.Roster.Sender(j.Self, j.Server) == SenderCoordinator
}

// mentioned is whether a human's content mentions this member, by id or `@room`.
func (j Judge) mentioned(content Value) bool {
	ids, room := mentions(content)
	return room || slices.Contains(ids, j.Self)
}

// agentBouncer is whether this member bounces a dropped agent event: it is an addressee, or no
// addressee can be read and it is the coordinator.
func (j Judge) agentBouncer(v Verdict) bool {
	to := addressees(v.Content)
	if v.Content.Key == "" {
		to = RawAddressees(v.Event.Content)
	}
	if len(to) == 0 {
		return j.coordinates()
	}
	return slices.Contains(to, j.Self)
}

// RawAddressees is the `to` of the main room's typed keys read best-effort from content that
// may not decode (Plan 00267 DESIGN A, row 29): every string of each known key's `to` list.
func RawAddressees(content Value) []string {
	var to []string
	for _, key := range MainKeys {
		obj, ok := content.Get(key)
		if !ok || obj.Kind() != KindObject {
			continue
		}
		list, ok := obj.Get("to")
		if !ok {
			continue
		}
		for _, item := range list.Items() {
			if item.Kind() == KindString {
				to = append(to, item.Str())
			}
		}
	}
	return to
}
