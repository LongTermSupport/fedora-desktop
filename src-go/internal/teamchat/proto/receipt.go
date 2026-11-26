package proto

import (
	"slices"
	"strings"
)

// KitLevel is this kit's protocol level, the `kit` of every typed object it sends (C7 of Plan
// 00267): additive inside major 1, so an older kit, which reads named fields only, ignores it.
// A sender seen without `kit` is an older kit, which neither sends receipts nor reads them.
const KitLevel = 1

// MaxKit bounds a received `kit`.
const MaxKit = 999999999

// MaxReceiptTo is the most user IDs a receipt's `to` names: the sender of the item, and at most
// a few the item came through (a relaying coordinator, a carried item's admin).
const MaxReceiptTo = 8

// maxUserID is the Matrix limit on a user ID's length.
const maxUserID = 255

// The states of a receipt (`teamchat.receipt`, A 7.8).
const (
	ReceiptDelivered = "delivered"
	ReceiptHeld      = "held"
	ReceiptBounced   = "bounced"
	ReceiptRecorded  = "recorded"
)

// The reasons a receipt gives that are not drop reasons: losses after a delivery was decided
// (bounced), and where an item waits (held).
const (
	ReasonRender      = "render"
	ReasonGone        = "gone"
	ReasonRemoved     = "removed"
	ReasonRedacted    = "redacted"
	ReasonUnheard     = "unheard"
	ReasonRoleChanged = "role-changed"
	ReasonExpired     = "expired"
	ReasonAbsent      = "absent"
	ReasonMissing     = "missing"
	ReasonBudget      = "budget"
	ReasonFull        = "full"
)

var (
	// ReceiptStates is the closed set of receipt states.
	ReceiptStates = []string{ReceiptDelivered, ReceiptHeld, ReceiptBounced, ReceiptRecorded}
	// BounceReasons are a bounced receipt's reasons: every drop reason, then the losses after
	// the verdict and the two ignores a mentioned member bounces.
	BounceReasons = append(slices.Clone(DropReasons),
		ReasonRender, ReasonGone, ReasonRemoved, ReasonRedacted, ReasonUnheard, IgnoreType, IgnoreEmpty, ReasonRoleChanged, ReasonExpired)
	// HeldReasons are a held receipt's reasons: the session is not taking deliveries, the
	// delivery was written and not seen in the transcript, the rate or budget window, the
	// pending list full, a forge check still to run.
	HeldReasons = []string{ReasonAbsent, ReasonMissing, ReasonRate, ReasonBudget, ReasonFull, ReasonForgeRate, ReasonUnresolved}
	// ReceiptReasons is the closed set of receipt reasons, each once.
	ReceiptReasons = union(BounceReasons, HeldReasons)
)

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Receipt is `teamchat.receipt` (A 7.8): an addressee's account of one item, posted in the
// reports room to the item's sender. Reason is "" for delivered and recorded.
type Receipt struct {
	Of     string
	To     []string
	State  string
	Reason string
	At     int64
}

// reasonsFor is the reasons a state takes; nil for a state that takes none.
func reasonsFor(state string) []string {
	switch state {
	case ReceiptHeld:
		return HeldReasons
	case ReceiptBounced:
		return BounceReasons
	}
	return nil
}

func decodeReceiptObject(c *Content, obj Value) string {
	f := readFields(obj, ReasonSchema)
	r := Receipt{
		Of:     f.str("of", true, ReasonSchema),
		To:     sortedStrs(f.strs("to", true, ReasonSchema)),
		State:  f.str("state", true, ReasonSchema),
		Reason: f.str("reason", false, ReasonSchema),
		At:     f.integer("at", true, -1, ReasonSchema),
	}
	reasons := reasonsFor(r.State)
	switch {
	case f.rule != "" || !ValidEventID(r.Of) || r.At < 0 || !slices.Contains(ReceiptStates, r.State):
		return ReasonSchema
	case !distinctList(r.To, 1, MaxReceiptTo, userIDForm):
		return ReasonSchema
	case f.has("reason") != (reasons != nil) || reasons != nil && !slices.Contains(reasons, r.Reason):
		return ReasonSchema
	}
	c.Receipt = r
	return ""
}

// userIDForm is a user ID's form on any server: `@<localpart>:<server>`, within Matrix's length.
func userIDForm(id string) bool {
	rest, ok := strings.CutPrefix(id, "@")
	local, host, found := strings.Cut(rest, ":")
	return ok && found && local != "" && host != "" && len(id) <= maxUserID
}

// ReceiptContent is a receipt's event content: quiet (`m.notice`, no mentions), its `body` a
// line for a human reading the reports room, the typed object carrying the kit. It is refused
// `schema` when a receiver would not decode it.
func ReceiptContent(r Receipt) (Value, error) {
	obj := []Member{M("v", Int(WireMajor)), M("of", Str(r.Of)), M("to", strArray(sortedStrs(r.To))), M("state", Str(r.State))}
	body := "receipt: " + r.State
	if r.Reason != "" {
		obj = append(obj, M("reason", Str(r.Reason)))
		body += " (" + r.Reason + ")"
	}
	obj = append(obj, M("at", Int(r.At)))
	content := Obj(M("msgtype", Str(MsgNotice)), M("body", Str(body+" "+r.Of)), M(keyMentions, Obj()), M(KeyReceipt, kitted(obj)))
	if _, err := decodeSent(content, ReportKeys); err != nil {
		return Value{}, err
	}
	return content, nil
}

// ReceiptOwed is the loop guard: a receipt, whether or not it decodes, is never itself given a
// receipt, so two links never answer each other. Every other event may be. One receipt per
// (item, addressee) is the receiver's bookkeeping, not this guard.
func ReceiptOwed(content Value) bool {
	_, receipt := content.Get(KeyReceipt)
	return !receipt
}
