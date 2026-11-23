package proto

import (
	"crypto/sha256"
	"encoding/hex"
)

// RelayText is a relay's text and body (A 7.3): a pointer to the post, never its words, which
// the receiver fetches itself.
func RelayText(n int64) string {
	return "A human answered " + QuestionName(int(n)) + " in the questions room; your link fetches the post itself."
}

// RelayTxn is the transaction ID of the relay of post to seat (A 10.6): derived from both, so a
// relay sent again after a restart is the same transaction and lands once.
func RelayTxn(post, seat string) string {
	sum := sha256.Sum256([]byte("relay\x00" + post + "\x00" + seat))
	return "tc" + hex.EncodeToString(sum[:16])
}

// checkRelay is rule 08 for a text with `relay`: only the coordinator sends one (`role`), to
// exactly one agent member (`target`).
func checkRelay(class SenderClass, to []string, r Roster) string {
	switch {
	case class != SenderCoordinator:
		return ReasonRole
	case len(to) != 1 || !r.AddressableAgent(to[0]):
		return ReasonTarget
	}
	return ""
}

// Relayed is A 10.6's relay check, by a member that does not coordinate, of post: the event
// fetched by ID from questions (j is that room's judge) for relay, a delivered text with
// `relay`. It is a HUMAN verdict carrying the relay's account when post is the event named and
// a team human's message; otherwise the relay is dropped `provenance`, never shown as PEER.
func (j Judge) Relayed(relay Verdict, post Value) Verdict {
	r := relay.Content.Text.Relay
	if r == nil {
		return drop(relay, ReasonProvenance)
	}
	v := j.Fetched(post)
	if v.Outcome != Deliver || v.Event.ID != r.ID {
		return drop(relay, ReasonProvenance)
	}
	v.Relayed = &Relayed{By: relay.Event.Sender, Q: r.Q}
	return v
}

// Fetched judges a questions-room event a member that does not coordinate fetched by ID (a
// relayed post, or one restored after a restart): a team human's message, its edit only of the
// same sender's message in this room, is delivered HUMAN; anything else is dropped
// `provenance`. It is not stale and not addressed: the coordinator chose to send it on.
func (j Judge) Fetched(post Value) Verdict {
	ev, ok := decodeEvent(post)
	v := Verdict{Event: ev}
	if !ok || stringMember(post, "type") != MessageType {
		return drop(v, ReasonProvenance)
	}
	if _, isState := post.Get("state_key"); isState {
		return drop(v, ReasonProvenance)
	}
	if _, carried := ev.Content.Get(KeyCarried); carried {
		return drop(v, ReasonProvenance)
	}
	v.Class = j.Roster.Sender(ev.Sender, j.Server)
	if v.Class != SenderHuman {
		return drop(v, ReasonProvenance)
	}
	h, reason := DecodeHuman(ev.Content)
	if reason != "" {
		return drop(v, ReasonProvenance)
	}
	if _, ok := j.original(h, ev.Sender); !ok {
		return drop(v, ReasonProvenance)
	}
	v.Human = h
	return deliver(v, KindHuman)
}

// relayedLine is A 13.2's `relayed by:` line.
func relayedLine(handle string) string {
	return "relayed by: " + handle + " (the question number is its claim; the text was fetched from questions)"
}
