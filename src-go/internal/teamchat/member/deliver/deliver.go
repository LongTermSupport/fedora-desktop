// Package deliver is the pure half of delivery into Claude Code (Plan 00209 SPEC-n3 DV2, DV3;
// B 2.7.2, 2.7.3): which team's items go next, and one team's pending items mapped to A 13's
// entries and fitted into one delivery. It does no I/O and reads no clock; the engine's loop
// owns the socket, the stores and the pace.
//
// The mapping takes each entry's fields from the item's verdict as the receive judged it, and
// its `from:` and `role:` from the caller's Namer (the verified roster, never the message).
// Every entry's slots are checked before the delivery is fitted: an entry that fails is
// dropped as `render` (Built.Failed), and a HUMAN one is named on the delivery's `not shown:`
// line (DV2). The nonce is drawn after the bodies are rendered (B 2.7.2).
package deliver

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The store's item kinds (state.Pending.Item).
const (
	itemHuman   = "human"
	itemPeer    = "peer"
	itemPing    = "ping"
	itemNotice  = "notice"
	itemTimeout = "timeout"
)

// Item is one pending item and what renders it: the verdict of its event (nil for a restored
// item not yet fetched again, which is left pending), or for a TIMEOUT the outbox ping and its
// silent addressee. Question is the question a questions-room entry is about, from the
// question index; nil is `question: none`.
type Item struct {
	Pending  state.Pending
	Verdict  *proto.Verdict
	Timeout  *Timeout
	Question *proto.EntryQuestion
}

// Timeout is a TIMEOUT's account: the ping it is about (none for a question's reminder), and
// the silent addressee's localpart, or proto.HumansWord for a reminder.
type Timeout struct {
	Ping   state.Ping
	Silent string
}

// Namer gives a sender's `from:` and `role:` from the verified roster: an agent's handle and
// its role, `admin` twice for the admin, and a human's name with no role.
type Namer func(userID string) (from, role string)

// Facts are the team's trailing facts held for its next delivery.
type Facts struct {
	Answered []proto.Answered
	HeldBack []proto.HeldBack
	Gap      bool
	Stopped  bool
}

// Built is one delivery: the fitted Delivery and its Text, empty when there is nothing to
// send. IDs are the items in the text, in its order; Failed the items whose entry failed a slot
// (dropped as `render`); Left the items still pending past the caps or awaiting a fetch.
type Built struct {
	Delivery proto.Delivery
	Text     string
	IDs      []string
	Failed   []string
	Left     []string
}

// Choose is the team whose delivery goes next (B 2.7.3): the one with the oldest pending HUMAN
// item, else the one with the oldest item of any kind; ties go to the lower team id. Handed and
// held items are not deliverable and are skipped. False when no team has a deliverable item.
func Choose(teams map[paths.TeamID][]state.Pending) (paths.TeamID, bool) {
	type oldest struct {
		team paths.TeamID
		ms   int64
		ok   bool
	}
	var human, anyKind oldest
	ids := make([]paths.TeamID, 0, len(teams))
	for t := range teams {
		ids = append(ids, t)
	}
	slices.SortFunc(ids, func(a, b paths.TeamID) int { return strings.Compare(a.String(), b.String()) })
	for _, t := range ids {
		for _, p := range teams[t] {
			if p.Handed != nil || p.Held {
				continue
			}
			if p.Item == itemHuman && (!human.ok || p.SentMS < human.ms) {
				human = oldest{t, p.SentMS, true}
			}
			if !anyKind.ok || p.SentMS < anyKind.ms {
				anyKind = oldest{t, p.SentMS, true}
			}
		}
	}
	if human.ok {
		return human.team, true
	}
	return anyKind.team, anyKind.ok
}

// Build maps team t's items to entries for this session's handle self, puts them in A 13.7's
// order, and fits the most that render within the caps beside the facts (proto.Delivery.Fit).
// With no entry and no trailing fact there is nothing to send: no nonce is drawn and Text is
// empty. An error is a nonce that could not be drawn or a fact that fails its slot.
func Build(t paths.TeamID, self string, items []Item, f Facts, from Namer, nonce func() (string, error)) (Built, error) {
	m := mapItems(t, self, items, from)
	out := Built{Failed: m.failed}
	d := proto.Delivery{
		Team: t.String(), Answered: f.Answered, HeldBack: f.HeldBack, NotShown: m.notShown, Gap: f.Gap, Stopped: f.Stopped,
		Pending: len(m.unfetched),
	}
	for _, r := range m.entries {
		d.Entries = append(d.Entries, r.entry)
	}
	unfetched, entries := m.unfetched, m.entries
	if len(d.Entries) == 0 && len(d.Answered) == 0 && len(d.HeldBack) == 0 && len(d.NotShown) == 0 && !d.Gap && !d.Stopped {
		out.Left = unfetched
		return out, nil
	}
	n, err := nonce()
	if err != nil {
		return Built{}, fmt.Errorf("deliver: the nonce: %w", err)
	}
	d.Nonce = n
	fitted, err := d.Fit()
	if err != nil {
		return Built{}, fmt.Errorf("deliver: team %s: %w", t, err)
	}
	text, err := fitted.Render()
	if err != nil {
		return Built{}, fmt.Errorf("deliver: team %s: %w", t, err)
	}
	for i, r := range entries {
		if i < len(fitted.Entries) {
			out.IDs = append(out.IDs, r.id)
		} else {
			out.Left = append(out.Left, r.id)
		}
	}
	out.Left = append(out.Left, unfetched...)
	out.Delivery, out.Text = fitted, text
	return out, nil
}

type ranked struct {
	entry proto.Entry
	id    string
}

// mapped is the items sorted out: the entries that render, in A 13.7's order; the items whose
// entry failed, HUMAN ones also named for `not shown:`; and the items still to be fetched.
type mapped struct {
	entries                     []ranked
	failed, notShown, unfetched []string
}

func mapItems(t paths.TeamID, self string, items []Item, from Namer) mapped {
	var m mapped
	for _, it := range items {
		if it.Pending.Item != itemTimeout && it.Verdict == nil {
			m.unfetched = append(m.unfetched, it.Pending.ID)
			continue
		}
		e, ok := entry(t, self, it, from)
		if ok {
			_, err := e.Lines()
			ok = err == nil
		}
		if !ok {
			m.failed = append(m.failed, it.Pending.ID)
			if it.Pending.Item == itemHuman {
				m.notShown = append(m.notShown, it.Pending.ID)
			}
			continue
		}
		m.entries = append(m.entries, ranked{e, it.Pending.ID})
	}
	slices.SortStableFunc(m.entries, func(a, b ranked) int {
		ra, rb := rank(a.entry.Kind), rank(b.entry.Kind)
		if ra != rb || a.entry.Kind == proto.KindTimeout {
			return cmp.Compare(ra, rb)
		}
		return cmp.Compare(a.entry.SentMs, b.entry.SentMs)
	})
	return m
}

// rank is the entry's group in A 13.7's order: HUMAN, then PEER and PING together, then
// TIMEOUT (kept in the order the caller gives, the order they fell due in), then NOTICE.
func rank(kind string) int {
	switch kind {
	case proto.KindHuman:
		return 0
	case proto.KindPeer, proto.KindPing:
		return 1
	case proto.KindTimeout:
		return 2
	}
	return 3
}

// sender is what the Namer gives for one user id.
type sender struct{ name, role string }

func ask(from Namer, userID string) sender {
	name, role := from(userID)
	return sender{name, role}
}

// entry maps one item to its entry; false for a kind no entry has, or a TIMEOUT with no account.
func entry(t paths.TeamID, self string, it Item, from Namer) (proto.Entry, bool) {
	p := it.Pending
	e := proto.Entry{
		Team: t.String(), Handle: self, EventID: p.ID, SentMs: p.SentMS, Repeat: p.Repeat, Restored: p.Restored,
		Questions: p.Room == proto.PurposeQuestions, Question: it.Question,
	}
	s := ask(from, p.From)
	switch p.Item {
	case itemHuman:
		h := it.Verdict.Human
		e.Kind, e.From, e.Body, e.Re, e.Revises = proto.KindHuman, s.name, h.Body, h.ReplyTo, h.Revises
	case itemPeer:
		c := it.Verdict.Content
		e.Kind, e.From, e.Role = proto.KindPeer, s.name, s.role
		e.Body, e.Re, e.Copy = c.Text.Text, re(*it.Verdict), copied(c.Text.To, from)
		if e.Body == "" {
			e.Body = c.Body
		}
	case itemPing:
		ping := it.Verdict.Content.Ping
		e.Kind, e.From, e.Role = proto.KindPing, s.name, s.role
		e.Verb, e.Ref, e.Branch = ping.Verb, ping.Ref, ping.Branch
		e.Re, e.Copy = re(*it.Verdict), copied(ping.To, from)
		if ping.Ref != "" && it.Verdict.Forge != nil {
			e.Trust = it.Verdict.Forge.Trust()
		}
	case itemNotice:
		e.Kind, e.From, e.Role = proto.KindNotice, s.name, s.role
		noticeFields(&e, *it.Verdict, from)
	case itemTimeout:
		if it.Timeout == nil {
			return proto.Entry{}, false
		}
		// The key is the ping's or the question's event id, `~`, and the silent addressee.
		if i := strings.IndexByte(p.ID, '~'); i >= 0 {
			e.EventID = p.ID[:i]
		}
		e.Kind, e.Restored, e.Silent, e.Verb = proto.KindTimeout, false, it.Timeout.Silent, it.Timeout.Ping.Verb
		if it.Timeout.Ping.Ref != nil {
			e.Ref = *it.Timeout.Ping.Ref
		}
	default:
		return proto.Entry{}, false
	}
	return e, true
}

// noticeFields are a NOTICE's event, subject and reason, or a carried item's account.
func noticeFields(e *proto.Entry, v proto.Verdict, from Namer) {
	if v.Carried {
		c := v.Content.Carried
		e.Carried = &proto.EntryCarried{Kind: c.Kind, From: c.From, SentMs: c.SentMs, PossibleDuplicate: c.PossibleDuplicate}
		e.Body = c.Text
		if c.Kind == proto.CarriedPing {
			e.Verb, e.Ref = c.Verb, c.Ref
		}
		return
	}
	n := v.Content.Notice
	e.Event = n.Event
	if n.Subject != "" {
		e.Subject = ask(from, n.Subject).name
	}
	switch n.Event {
	case proto.EventLeft:
		e.Reason = n.Reason
	case proto.EventAdded, proto.EventRole:
		e.Reason = n.Role
	}
}

// re is the event an agent's item replies to, or the ping an answer answers (A 13.2); a
// thread's fallback reply is not a reply.
func re(v proto.Verdict) string {
	if v.Answer != nil {
		return v.Answer.Ping
	}
	if v.Content.Relation.FallingBack {
		return ""
	}
	return v.Content.Relation.InReplyTo
}

// copied are the humans an agent's item is addressed to, beside this session: shown on its
// `copy:` line as sent to it for information.
func copied(to []string, from Namer) []string {
	var out []string
	for _, id := range to {
		if s := ask(from, id); s.role == "" {
			out = append(out, s.name)
		}
	}
	return out
}
