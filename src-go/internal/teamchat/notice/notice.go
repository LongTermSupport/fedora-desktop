// Package notice posts A's `disconnected` notice for a member whose session ended unseen
// (Plan 00209 SPEC-n2e.md NT1-NT2; DESIGN-v2-C-host.md 3.7.5 and 3.11.2 step 6, DESIGN-v2-B-member.md
// 2.11): as the subject with its own token, unless its latest own lifecycle notice in main since
// the time the host knew the seat held is `left` or `disconnected`, with transaction id
// `tc-disc-<run id>` so the host's calls and the next link on the seat post it once. B's
// `seat disconnected` and the health tick's child call it. Everything the member or its room
// can cause is an Outcome; only a transient failure is an error.
package notice

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The page bound of the read backwards through main (B 2.11).
const (
	MaxPages = 10
	PageSize = 100
)

// Client is the part of the subject's member client the notice uses.
type Client interface {
	RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error)
	Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error)
	Send(ctx context.Context, roomID, eventType, txnID string, content json.RawMessage) (string, error)
}

// Notice is what the notice is posted from: the team's anchors, the subject's handle and its
// own token (screened out of the body), the ended run and the time the host knew it held.
type Notice struct {
	Server, Team, MainRoom string
	Handle                 string
	Token                  string
	Run                    paths.RunID
	SinceMS                int64
}

// Outcome is the notice judged: posted (Found when the subject's own goodbye was already
// there), or not possible for good and why.
type Outcome struct {
	Posted, Found bool
	Why           string
}

// Why a notice cannot be posted, by class; never a value from the room.
const (
	whyToken      = "the subject's token was refused"
	whyForbidden  = "the homeserver forbade the post"
	whyRefused    = "the homeserver refused the call"
	whyUnverified = "the main room does not verify"
	whyInactive   = "the subject is not an active agent member of the roster"
	whyComposer   = "the notice does not compose"
)

// TxnID is the notice's transaction id for the ended run.
func TxnID(run paths.RunID) string { return "tc-disc-" + run.String() }

// Disconnected posts the subject's `disconnected` (NT1-NT2).
func Disconnected(ctx context.Context, c Client, n Notice) (Outcome, error) {
	self := proto.UserID(n.Handle, n.Server)
	raw, err := c.RoomState(ctx, n.MainRoom)
	if err != nil {
		return judged(ctx, err)
	}
	roster, why := verify(raw, n, self)
	if why == "" && roster.Members[self].Role == proto.RoleProbe {
		why = whyInactive
	}
	if why != "" {
		return Outcome{Why: why}, nil
	}
	found, err := goodbye(ctx, c, n, self)
	if err != nil {
		return judged(ctx, err)
	}
	if found {
		return Outcome{Posted: true, Found: true}, nil
	}
	content, composed := compose(roster, n, self)
	if !composed {
		return Outcome{Why: whyComposer}, nil
	}
	if _, err := c.Send(ctx, n.MainRoom, proto.MessageType, TxnID(n.Run), content); err != nil {
		return judged(ctx, err)
	}
	return Outcome{Posted: true}, nil
}

// verify is main's roster when main verifies under A 6.4 with the subject an unparked member,
// else why not.
func verify(raw []json.RawMessage, n Notice, self string) (proto.Roster, string) {
	r, err := proto.VerifyMain(stateEvents(raw), proto.Anchors{Server: n.Server, Team: n.Team, MainRoom: n.MainRoom, Self: self})
	if te, ok := errors.AsType[*proto.TrustError](err); ok && te.Check == "member" {
		return proto.Roster{}, whyInactive
	}
	if err != nil {
		return proto.Roster{}, whyUnverified
	}
	return r, ""
}

// compose is the notice's content, built by A's composer, which refuses what a receiver drops.
func compose(r proto.Roster, n Notice, self string) (json.RawMessage, bool) {
	out, err := proto.Composer{Server: n.Server, Self: self, Token: n.Token, Roster: r}.Notice(
		proto.Notice{Event: proto.EventDisconnected, Subject: self, To: addressees(r, self)}, display(n.Handle))
	if err != nil {
		return nil, false
	}
	return out.Content.Compact(), true
}

// judged is a failed call: transient (and a cancelled context) is an error, the rest an
// outcome by class.
func judged(ctx context.Context, err error) (Outcome, error) {
	me, isMatrix := errors.AsType[*matrix.Error](err)
	switch {
	case ctx.Err() != nil || !isMatrix || me.Transient() || errors.Is(err, matrix.ErrRateLimited) || errors.Is(err, matrix.ErrUnreachable):
		return Outcome{}, err
	case errors.Is(err, matrix.ErrAuthRefused):
		return Outcome{Why: whyToken}, nil
	case errors.Is(err, matrix.ErrForbidden):
		return Outcome{Why: whyForbidden}, nil
	}
	return Outcome{Why: whyRefused}, nil
}

// goodbye reads main backwards, at most MaxPages of PageSize, for the subject's latest own
// lifecycle notice since n.SinceMS: true when it is `left` or `disconnected`.
func goodbye(ctx context.Context, c Client, n Notice, self string) (bool, error) {
	req := matrix.MessagesRequest{Dir: matrix.Backward, Limit: PageSize}
	for range MaxPages {
		page, err := c.Messages(ctx, n.MainRoom, req)
		if err != nil {
			return false, err
		}
		for _, raw := range page.Chunk {
			ev, ok := lifecycle(raw)
			switch {
			case !ok:
			case ev.ts < n.SinceMS:
				return false, nil
			case ev.sender == self && ev.subject == self:
				return ev.event == proto.EventLeft || ev.event == proto.EventDisconnected, nil
			}
		}
		if page.End == "" || len(page.Chunk) == 0 {
			return false, nil
		}
		req.From = page.End
	}
	return false, nil
}

// event is a room event as the scan reads it; event and subject are set only for a lifecycle
// notice that decodes under A 7.1.
type event struct {
	sender, event, subject string
	ts                     int64
}

// lifecycle reads a timeline event: its sender and time, and when it is a member's lifecycle
// notice that decodes, its event and subject. ok is false for an event with no sender or time.
func lifecycle(raw []byte) (event, bool) {
	v, err := proto.Parse(raw)
	if err != nil || v.Kind() != proto.KindObject {
		return event{}, false
	}
	sender, okSender := v.Get("sender")
	ts, okTS := v.Get("origin_server_ts")
	if !okSender || sender.Kind() != proto.KindString || !okTS || ts.Kind() != proto.KindInt {
		return event{}, false
	}
	ev := event{sender: sender.Str(), ts: ts.Int()}
	typ, okType := v.Get("type")
	content, okContent := v.Get("content")
	if !okType || typ.Kind() != proto.KindString || typ.Str() != proto.MessageType || !okContent {
		return ev, true
	}
	if dc, reason := proto.DecodeContent(content, []string{proto.KeyNotice}); reason == "" && proto.MemberNotice(dc.Notice.Event) {
		ev.event, ev.subject = dc.Notice.Event, dc.Notice.Subject
	}
	return ev, true
}

// addressees are A 7.4's `to`: the humans and the coordinator when it is not parked, never the
// subject.
func addressees(r proto.Roster, self string) []string {
	to := slices.Clone(r.Humans)
	if coordinator, open := r.Coordinator(); open && coordinator != self {
		to = append(to, coordinator)
	}
	slices.Sort(to)
	return slices.DeleteFunc(to, func(id string) bool { return id == self })
}

// display is the notice's body: the subject's handle and A 7.4's gloss.
func display(handle string) string { return handle + ": the member's session ended without a goodbye" }

// stateEvents is a room's state as proto's trust checks take it; an event that does not read
// is left out, so the checks find what is missing.
func stateEvents(raw []json.RawMessage) []proto.StateEvent {
	out := make([]proto.StateEvent, 0, len(raw))
	for _, r := range raw {
		v, err := proto.Parse(r)
		if err != nil || v.Kind() != proto.KindObject {
			continue
		}
		strs := map[string]string{}
		for _, name := range []string{"type", "state_key", "sender", "event_id"} {
			if f, ok := v.Get(name); ok && f.Kind() == proto.KindString {
				strs[name] = f.Str()
			}
		}
		content, ok := v.Get("content")
		if !ok || content.Kind() != proto.KindObject || len(strs) != 4 {
			continue
		}
		out = append(out, proto.StateEvent{Type: strs["type"], StateKey: strs["state_key"], Sender: strs["sender"], EventID: strs["event_id"], Content: content})
	}
	return out
}
