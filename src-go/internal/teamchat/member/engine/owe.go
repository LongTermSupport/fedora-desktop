package engine

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// receiptTo is who item p's receipt of state st goes to, and whether one is owed (Plan 00267
// C2): a PEER or PING item's sender, a member, is owed every state; a HUMAN's only a bounce, its
// m.read being its delivered; a TIMEOUT (this member's own clock) and a NOTICE (the admin's)
// are owed none, and nothing is owed to this member itself.
func receiptTo(s *state.Store, p state.Pending, st string) (string, bool) {
	switch {
	case p.From == "" || p.From == s.UserID:
		return "", false
	case p.Item == records.KindPeer || p.Item == records.KindPing:
		return p.From, true
	case p.Item == records.KindHuman:
		return p.From, st == proto.ReceiptBounced
	}
	return "", false
}

// owe queues item p's receipt of state st (with reason, "" for delivered and recorded) to its
// sender at now, when one is owed; the queue's refusal is the error, so the write that owes it
// is refused whole and nothing moves ahead of the store.
func owe(s *state.Store, p state.Pending, st, reason string, now int64) error {
	to, owed := receiptTo(s, p, st)
	if !owed {
		return nil
	}
	return oweTo(s, p.ID, to, st, reason, now)
}

// oweTo queues the receipt of item of to the sender to: a terminal state (delivered, bounced,
// recorded) replaces a held one still queued for the item and sender, which it supersedes.
func oweTo(s *state.Store, of, to, st, reason string, now int64) error {
	if st != proto.ReceiptHeld {
		s.Owed = slices.DeleteFunc(s.Owed, func(o state.Owed) bool {
			return o.Of == of && o.To == to && o.State == proto.ReceiptHeld
		})
	}
	return s.AddOwed(state.Owed{Of: of, To: to, State: st, Reason: reason, AtMS: now})
}

// bounceItem is item p, lost after its verdict delivered it, bounced with reason at now: listed
// in the drop ledger and a bounced receipt owed its sender, when one is owed (receiptTo).
func bounceItem(s *state.Store, p state.Pending, reason string, now int64) error {
	if _, owed := receiptTo(s, p, proto.ReceiptBounced); !owed {
		return nil
	}
	return bounce(s, team.Bounce{ID: p.ID, Room: p.Room, From: p.From, SentMS: p.SentMS, Reason: reason}, now)
}

// bounceItems is bounceItem for each of ps.
func bounceItems(s *state.Store, ps []state.Pending, reason string, now int64) error {
	for _, p := range ps {
		if err := bounceItem(s, p, reason, now); err != nil {
			return err
		}
	}
	return nil
}

// oweRecorded is the recorded receipt each answer of b owes (Plan 00267 C2, row 37): an ack, or
// a late answer, to an own ping the outbox holds, from one of its addressees, that is not one
// of b's items (an answer delivered as an item is settled by its delivery).
func oweRecorded(s *state.Store, b team.Batch, now int64) error {
	for _, a := range b.Answered {
		p := findPing(s, a.Ping)
		if p == nil || !slices.Contains(p.To, a.From) {
			continue
		}
		if slices.ContainsFunc(b.Items, func(it team.Item) bool { return it.Pending.ID == a.ID }) {
			continue
		}
		if a.Verb != proto.VerbAck && a.OriginMS <= p.DueMS {
			continue
		}
		if err := oweTo(s, a.ID, a.From, proto.ReceiptRecorded, "", now); err != nil {
			return err
		}
	}
	return nil
}

// removedOwed is what a removal posts before the store goes: the receipts s owes already, then a
// bounce `removed` for each item the team still owes the session (pending, handed or awaiting).
func removedOwed(s *state.Store, now int64) []state.Owed {
	out := slices.Clone(s.Owed)
	for _, p := range s.Pending {
		if to, owed := receiptTo(s, p, proto.ReceiptBounced); owed {
			out = append(out, state.Owed{Of: p.ID, To: to, State: proto.ReceiptBounced, Reason: proto.ReasonRemoved, AtMS: now})
		}
	}
	return out
}
