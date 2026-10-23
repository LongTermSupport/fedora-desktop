package engine

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// namer gives the short `from` an agent can pass to --to (B 2.2.2, P69): a human's or the
// admin's localpart; an agent's short name when no other member of the verified roster has
// it, else its whole handle.
type namer struct {
	server   string
	roster   proto.Roster
	verified bool
}

func (n namer) name(id string) string {
	if h, ok := proto.AgentHandle(id, n.server); ok {
		if n.verified && n.uniqueShort(h.Short()) {
			return h.Short()
		}
		return h.String()
	}
	if local, ok := proto.Localpart(id, n.server); ok {
		return local
	}
	return id
}

func (n namer) uniqueShort(short string) bool {
	count := 0
	for id := range n.roster.Members {
		if h, ok := proto.AgentHandle(id, n.server); ok && h.Short() == short {
			count++
		}
	}
	return count == 1
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// trust is a ping's `trust`: its reference's proven class, "-" for a ping with none.
func trust(v proto.Verdict) string {
	if v.Forge == nil {
		return "-"
	}
	return v.Forge.Trust()
}

// itemRecord is a pending item's record, from its verdict in memory (B 2.7.3 "Content"); a
// TIMEOUT is made from the store alone, its key and the outbox ping it names.
func itemRecord(t paths.TeamID, p state.Pending, v proto.Verdict, n namer, pings []state.Ping) records.Record {
	switch p.Item {
	case records.KindHuman:
		return records.Human{
			Team: t, ID: p.ID, From: n.name(p.From), FromID: p.From, SentMS: p.SentMS, Room: p.Room,
			Text: v.Human.Body, Revises: optional(v.Human.Revises), Restored: p.Restored,
		}
	case records.KindPeer:
		text := v.Content.Text.Text
		if text == "" {
			text = v.Content.Body
		}
		return records.Peer{Team: t, ID: p.ID, From: n.name(p.From), SentMS: p.SentMS, Text: text, Restored: p.Restored}
	case records.KindPing:
		r := records.Ping{
			Team: t, ID: p.ID, From: n.name(p.From), SentMS: p.SentMS, Verb: v.Content.Ping.Verb,
			Ref: optional(v.Content.Ping.Ref), Trust: trust(v), Branch: optional(v.Content.Ping.Branch), Restored: p.Restored,
		}
		if v.Answer != nil {
			r.Re = optional(v.Answer.Ping)
		}
		return r
	case records.KindNotice:
		r := records.Notice{
			Team: t, ID: p.ID, From: n.name(p.From), SentMS: p.SentMS, Event: v.Content.Notice.Event,
			Subject: n.name(v.Content.Notice.Subject), Reason: optional(v.Content.Notice.Reason),
		}
		if v.Carried {
			c := v.Content.Carried
			r.Carried = &records.Carried{
				Kind: c.Kind, Verb: optional(c.Verb), Ref: optional(c.Ref), From: c.From, SentMS: c.SentMs,
				V1Event: c.V1Event, To: c.To, Text: c.Text, PossibleDuplicate: c.PossibleDuplicate,
			}
		}
		return r
	}
	return timeoutRecord(t, p, pings)
}

// timeoutRecord is a TIMEOUT: its key is the ping's event id and the silent addressee
// (proto.TimeoutKey).
func timeoutRecord(t paths.TeamID, p state.Pending, pings []state.Ping) records.Timeout {
	r := records.Timeout{Team: t, ID: p.ID, Restored: p.Restored}
	event, silent, keyed := strings.Cut(p.ID, "~")
	if !keyed {
		return r
	}
	r.Silent = silent
	for _, ping := range pings {
		if ping.ID == event {
			r.Verb, r.Ref = ping.Verb, ping.Ref
		}
	}
	return r
}

// needsContent is whether an item is rendered from its event: every kind but a TIMEOUT.
func needsContent(p state.Pending) bool { return p.Item != records.KindTimeout }
