package teamsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	notInMain    exits.Template = "event {} is not in the team's main room"
	notAuthentic exits.Template = "event {} is from no member of the team, or does not read as its sender's"
	notARoom     exits.Template = "{} is not a room this member reads"

	verbUnreachable exits.Template = "the homeserver of team {} did not answer; the link keeps retrying"
	verbToken       exits.Template = "the homeserver of team {} refused the member's token"
	verbForbidden   exits.Template = "the homeserver of team {} forbade the request"
	verbRate        exits.Template = "the homeserver of team {} is rate limiting the member"
)

// verbErr is a verb's homeserver failure as B 2.2.4's exit, never internal: a refused token, a
// forbidden request, a rate limit, or no usable answer. Anything else, a context's end and an
// exits error included, is returned as it is.
func (t *Team) verbErr(err error) error {
	var (
		code string
		msg  exits.Template
	)
	switch {
	case err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, matrix.ErrAuthRefused):
		code, msg = exits.CodeToken, verbToken
	case errors.Is(err, matrix.ErrForbidden):
		code, msg = exits.CodeForbidden, verbForbidden
	case errors.Is(err, matrix.ErrRateLimited):
		code, msg = exits.CodeServerRate, verbRate
	case errors.Is(err, matrix.ErrUnreachable) || errors.Is(err, matrix.ErrRefused):
		code, msg = exits.CodeUnreachable, verbUnreachable
	default:
		return err
	}
	t.log.Printf("a verb's call failed: %s", matrix.Class(err))
	return exits.New(code, &t.cfg.Team, msg, exits.Team(t.cfg.Team))
}

// maxHistory is the most events one History reads.
const maxHistory = 100

// message is an event of a room as LN6 reads it: whose it is and what it says.
type message struct {
	id, sender string
	ts         int64
	kind       string
	text       string
	human      proto.Human
	content    proto.Content
}

// readMessage judges an event's authenticity by its sender's class in the roster: a human's
// message as DecodeHuman reads it, an agent's or the admin's content as DecodeContent does.
// Addressing is not judged.
func readMessage(raw json.RawMessage, r proto.Roster, server string) (message, bool) {
	v, err := proto.Parse(raw)
	if err != nil || str(v, "type") != proto.MessageType {
		return message{}, false
	}
	if _, isState := v.Get("state_key"); isState {
		return message{}, false
	}
	m := message{id: str(v, "event_id"), sender: str(v, "sender"), ts: originTs(v)}
	content := member(v, "content")
	switch r.Sender(m.sender, server) {
	case proto.SenderHuman:
		h, reason := proto.DecodeHuman(content)
		m.kind, m.text, m.human = records.KindHuman, h.Body, h
		return m, reason == ""
	case proto.SenderAdmin:
		c, reason := proto.DecodeContent(content, proto.AdminMainKeys)
		m.kind, m.text, m.content = records.KindNotice, c.Body, c
		return m, reason == "" && c.Key == proto.KeyNotice
	case proto.SenderCoordinator, proto.SenderWorker, proto.SenderProbe:
		c, reason := proto.DecodeContent(content, proto.MainKeys)
		m.content, m.text = c, c.Body
		switch c.Key {
		case proto.KeyPing:
			m.kind = records.KindPing
		case proto.KeyText:
			m.kind, m.text = records.KindPeer, c.Text.Text
		default:
			m.kind = records.KindNotice
		}
		return m, reason == ""
	case proto.SenderNone:
	}
	return message{}, false
}

// roomOf is a room purpose's room id: main, or the questions room the roster names.
func (t *Team) roomOf(room string, a proto.Anchors, r proto.Roster) (string, error) {
	switch {
	case room == proto.PurposeMain:
		return a.MainRoom, nil
	case room == proto.PurposeQuestions && r.Rooms.Questions != "":
		return r.Rooms.Questions, nil
	}
	return "", exits.New(exits.CodeUsage, &t.cfg.Team, notARoom, exits.Name(room))
}

// rosterFor is the roster last verified, or main verified now when none is.
func (t *Team) rosterFor(ctx context.Context, c *matrix.Client, a proto.Anchors) (proto.Roster, error) {
	if r, ok := t.Roster(); ok {
		return r, nil
	}
	r, content, err := verifyMain(ctx, c, a)
	if err != nil {
		return proto.Roster{}, err
	}
	t.mu.Lock()
	t.roster, t.verified = r, true
	t.mu.Unlock()
	t.saveRoster(ctx, r.Rev, content)
	return r, nil
}

// Verify is LN6: the event re-fetched from main and judged authentic, with its full text and
// that text's SHA-256.
func (t *Team) Verify(ctx context.Context, id string) (records.Verified, error) {
	v, err := t.verify(ctx, id)
	return v, t.verbErr(err)
}

func (t *Team) verify(ctx context.Context, id string) (records.Verified, error) {
	c, pair, err := t.current()
	if err != nil {
		return records.Verified{}, err
	}
	a := anchorsOf(t.cfg.Team, pair)
	r, err := t.rosterFor(ctx, c, a)
	if err != nil {
		return records.Verified{}, err
	}
	raw, err := c.GetEvent(ctx, a.MainRoom, id)
	if errors.Is(err, matrix.ErrNotFound) {
		return records.Verified{}, exits.New(exits.CodeNotFound, &t.cfg.Team, notInMain, exits.Name(id))
	}
	if err != nil {
		return records.Verified{}, err
	}
	m, ok := readMessage(raw, r, a.Server)
	if !ok || m.id != id {
		return records.Verified{}, exits.New(exits.CodeUntrusted, &t.cfg.Team, notAuthentic, exits.Name(id))
	}
	sum := sha256.Sum256([]byte(m.text))
	return records.Verified{
		Team: t.cfg.Team, ID: id, Item: m.kind, From: m.sender, Room: proto.PurposeMain, Text: m.text,
		BodySHA256: hex.EncodeToString(sum[:]), SentMS: m.ts,
	}, nil
}

// History is LN6: a room's messages read backwards, at most limit (and maxHistory), given oldest
// first; it consumes nothing. An event that does not read as its sender's is left out.
func (t *Team) History(ctx context.Context, room string, limit int) ([]records.History, error) {
	hs, err := t.history(ctx, room, limit)
	return hs, t.verbErr(err)
}

func (t *Team) history(ctx context.Context, room string, limit int) ([]records.History, error) {
	c, pair, err := t.current()
	if err != nil {
		return nil, err
	}
	a := anchorsOf(t.cfg.Team, pair)
	r, err := t.rosterFor(ctx, c, a)
	if err != nil {
		return nil, err
	}
	roomID, err := t.roomOf(room, a, r)
	if err != nil {
		return nil, err
	}
	page, err := c.Messages(ctx, roomID, matrix.MessagesRequest{Dir: matrix.Backward, Limit: min(max(limit, 1), maxHistory), Types: []string{proto.MessageType}})
	if err != nil {
		return nil, err
	}
	var out []records.History
	for _, raw := range slices.Backward(page.Chunk) {
		if m, ok := readMessage(raw, r, a.Server); ok {
			out = append(out, t.historyOf(m, room, a))
		}
	}
	return out, nil
}

func (t *Team) historyOf(m message, room string, a proto.Anchors) records.History {
	dir := "in"
	if m.sender == a.Self {
		dir = "out"
	}
	from := m.sender
	if local, ok := proto.Localpart(m.sender, a.Server); ok {
		from = local
	}
	var item records.Record
	switch m.kind {
	case records.KindHuman:
		item = records.Human{Team: t.cfg.Team, ID: m.id, From: from, FromID: m.sender, SentMS: m.ts, Room: room, Text: m.text, Revises: optional(m.human.Revises)}
	case records.KindPeer:
		item = records.Peer{Team: t.cfg.Team, ID: m.id, From: from, SentMS: m.ts, Text: m.text}
	case records.KindPing:
		p := m.content.Ping
		ping := records.Ping{Team: t.cfg.Team, ID: m.id, From: from, SentMS: m.ts, Verb: p.Verb, Ref: optional(p.Ref), Branch: optional(p.Branch)}
		if p.Ref != "" {
			// History proves no reference: an unproven one is never shown trusted.
			ping.Trust = proto.TrustExternal
		}
		item = ping
	default:
		n := m.content.Notice
		item = records.Notice{Team: t.cfg.Team, ID: m.id, From: from, SentMS: m.ts, Event: n.Event, Subject: n.Subject, Reason: optional(n.Reason)}
	}
	return records.History{Team: t.cfg.Team, Dir: dir, Item: item}
}

// gone is err as a Refetch whose item can never be judged again.
func gone(err error) error { return fmt.Errorf("%w: %w", team.ErrItemGone, err) }

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Refetch judges a restored item again (B 2.7.3): the event fetched, its relations resolved as
// a sync's are, over a fresh flood window and a Lookup that does not count the item as seen. An
// event the server answers 404 or 403 for, one that does not parse, and a room other than main
// are team.ErrItemGone: the item can never be judged again.
func (t *Team) Refetch(ctx context.Context, room, id string) (proto.Verdict, error) {
	c, pair, err := t.current()
	if err != nil {
		return proto.Verdict{}, err
	}
	s := &sess{t: t, c: c, pair: pair, a: anchorsOf(t.cfg.Team, pair)}
	if s.roster, err = t.rosterFor(ctx, c, s.a); err != nil {
		return proto.Verdict{}, err
	}
	if room != proto.PurposeMain {
		return proto.Verdict{}, gone(exits.New(exits.CodeUsage, &t.cfg.Team, notARoom, exits.Name(room)))
	}
	raw, err := c.GetEvent(ctx, s.a.MainRoom, id)
	if errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden) {
		return proto.Verdict{}, gone(err)
	}
	if err != nil {
		return proto.Verdict{}, err
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Verdict{}, gone(exits.New(exits.CodeUntrusted, &t.cfg.Team, notAuthentic, exits.Name(id)))
	}
	relIDs := proto.RelationIDs(v, s.roster)
	view, err := t.cfg.Engine.View(t.cfg.Team, []string{id}, relIDs, s.roster)
	if err != nil {
		return proto.Verdict{}, err
	}
	rels := make([]relation, 0, len(relIDs))
	for _, rid := range relIDs {
		rels = append(rels, relation{event: id, id: rid})
	}
	resolved, err := t.res.resolve(ctx, s.resolveInput(view), rels)
	if err != nil {
		return proto.Verdict{}, err
	}
	flood := proto.Window{Times: map[string][]int64{}}
	vs := []proto.Verdict{t.judge(proto.Judge{
		Server: s.a.Server, Self: s.a.Self, RoomID: s.a.MainRoom, Purpose: proto.PurposeMain, Roster: s.roster,
		NowMs: t.cfg.Clock.WallMS(), Lookup: &memLookup{view: view, resolved: resolved, judged: map[string]bool{}, skip: id}, Flood: &flood,
	}, v)}
	// The same check as a sync's (n3d-D12): a restored ping that no longer proves is a drop.
	news := t.forgeCheck(ctx, s.roster.Forge, vs)
	if err := ctx.Err(); err != nil {
		return proto.Verdict{}, err
	}
	t.noteForge(news)
	return vs[0], nil
}

// Notices is the team's matrix calls for the ends; it works before Run and after it returns.
func (t *Team) Notices() team.Notices { return notices{t} }

// Roster is the roster last verified.
func (t *Team) Roster() (proto.Roster, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.roster, t.verified
}

type notices struct{ t *Team }

func (n notices) RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error) {
	c, _, err := n.t.current()
	if err != nil {
		return nil, err
	}
	return c.RoomState(ctx, roomID)
}

func (n notices) Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error) {
	c, _, err := n.t.current()
	if err != nil {
		return matrix.MessagesPage{}, err
	}
	return c.Messages(ctx, roomID, req)
}

func (n notices) Send(ctx context.Context, roomID, eventType, txnID string, content json.RawMessage) (string, error) {
	c, _, err := n.t.current()
	if err != nil {
		return "", err
	}
	return c.Send(ctx, roomID, eventType, txnID, content)
}

func (n notices) SetPresence(ctx context.Context, userID, presence string) error {
	c, _, err := n.t.current()
	if err != nil {
		return err
	}
	return c.SetPresence(ctx, userID, presence)
}

// Anchors are the pair's, empty when it cannot be read.
func (n notices) Anchors() proto.Anchors {
	p, err := n.t.readPair(n.t.cfg.Root, n.t.cfg.Seat)
	if err != nil {
		return proto.Anchors{}
	}
	return anchorsOf(n.t.cfg.Team, p)
}

func (n notices) Roster() (proto.Roster, bool) { return n.t.Roster() }

func (n notices) RecordOwnSend(id string, r proto.Related) { n.t.res.recordOwnSend(id, r) }

// Token is the pair's token, empty when it cannot be read.
func (n notices) Token() string {
	p, err := n.t.readPair(n.t.cfg.Root, n.t.cfg.Seat)
	if err != nil {
		return ""
	}
	return p.Token.Reveal()
}
