package teamsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	notInMain    exits.Template = "event {} is not in the team's main room"
	notInRooms   exits.Template = "event {} is not in the team's main room, nor in a questions room this member reads"
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
	t.log.Printf("a verb's call failed: %s", matrix.Cause(err))
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

// readRoomMessage is readMessage for an event of room purpose: in questions a human's message,
// or the coordinator's or the admin's question or thread post (A 10.2, 10.3); in main,
// readMessage's; in any other room, no message.
func readRoomMessage(raw json.RawMessage, r proto.Roster, server, purpose string) (message, bool) {
	switch purpose {
	case proto.PurposeMain:
		return readMessage(raw, r, server)
	case proto.PurposeQuestions:
		return readQuestionsMessage(raw, r, server)
	default:
		return message{}, false
	}
}

// readQuestionsMessage is readRoomMessage's reading of an event of the questions room.
func readQuestionsMessage(raw json.RawMessage, r proto.Roster, server string) (message, bool) {
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
	case proto.SenderCoordinator, proto.SenderAdmin:
		c, reason := proto.DecodeContent(content, proto.QuestionKeys)
		m.kind, m.text, m.content = records.KindNotice, c.Body, c
		return m, reason == ""
	case proto.SenderWorker, proto.SenderProbe, proto.SenderNone:
	}
	return message{}, false
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

// Verify is LN6: the event re-fetched from room (main or questions; nil is main, then questions
// when main has no such event, R10) and judged authentic, with its full text, that text's
// SHA-256, whether its sender is a team human, and the question it names.
func (t *Team) Verify(ctx context.Context, id string, room *string) (records.Verified, error) {
	v, err := t.verify(ctx, id, room)
	return v, t.verbErr(err)
}

func (t *Team) verify(ctx context.Context, id string, room *string) (records.Verified, error) {
	c, pair, err := t.current()
	if err != nil {
		return records.Verified{}, err
	}
	a := anchorsOf(t.cfg.Team, pair)
	r, err := t.rosterFor(ctx, c, a)
	if err != nil {
		return records.Verified{}, err
	}
	rooms := []string{proto.PurposeMain}
	switch {
	case room != nil:
		rooms = []string{*room}
	case r.Rooms.Questions != "":
		rooms = append(rooms, proto.PurposeQuestions)
	}
	for _, purpose := range rooms {
		roomID, err := t.roomOf(purpose, a, r)
		if err != nil {
			return records.Verified{}, err
		}
		raw, err := t.getEvent(ctx, c, purpose, roomID, id, r, a.Self)
		// A member outside questions is refused its events: to it they are not there.
		if errors.Is(err, matrix.ErrNotFound) || purpose == proto.PurposeQuestions && errors.Is(err, matrix.ErrForbidden) {
			continue
		}
		if err != nil {
			return records.Verified{}, err
		}
		m, ok := readRoomMessage(raw, r, a.Server, purpose)
		if !ok || m.id != id {
			return records.Verified{}, exits.New(exits.CodeUntrusted, &t.cfg.Team, notAuthentic, exits.Name(id))
		}
		return t.verifiedRecord(ctx, c, m, purpose, roomID, r, a), nil
	}
	msg := notInMain
	if slices.Contains(rooms, proto.PurposeQuestions) {
		msg = notInRooms
	}
	return records.Verified{}, exits.New(exits.CodeNotFound, &t.cfg.Team, msg, exits.Name(id))
}

// getEvent is GetEvent of id in roomID, the room of purpose. A member that does not coordinate
// reads questions by event ID only (A 4.1, 10.6): refused, it joins the room by the invite every
// non-parked member holds and reads again. It never syncs the room; the coordinator's own join
// is QS2's. Any other room is refused.
func (t *Team) getEvent(ctx context.Context, c *matrix.Client, purpose, roomID, id string, r proto.Roster, self string) (json.RawMessage, error) {
	switch purpose {
	case proto.PurposeMain:
		return c.GetEvent(ctx, roomID, id)
	case proto.PurposeQuestions:
		return t.getQuestionsEvent(ctx, c, roomID, id, r, self)
	default:
		return nil, exits.New(exits.CodeUsage, &t.cfg.Team, notARoom, exits.Name(purpose))
	}
}

// getQuestionsEvent is getEvent of id in the questions room roomID.
func (t *Team) getQuestionsEvent(ctx context.Context, c *matrix.Client, roomID, id string, r proto.Roster, self string) (json.RawMessage, error) {
	raw, err := c.GetEvent(ctx, roomID, id)
	if !errors.Is(err, matrix.ErrForbidden) {
		return raw, err
	}
	if coordinator, ok := r.Coordinator(); ok && coordinator == self {
		return raw, err
	}
	if _, joinErr := c.Join(ctx, roomID); joinErr != nil {
		t.log.Printf("the questions room was not joined to read %s: %s", id, matrix.Cause(joinErr))
		return raw, err
	}
	return c.GetEvent(ctx, roomID, id)
}

// verifiedRecord is m, read from room purpose, as verify's record.
func (t *Team) verifiedRecord(ctx context.Context, c *matrix.Client, m message, purpose, roomID string, r proto.Roster, a proto.Anchors) records.Verified {
	sum := sha256.Sum256([]byte(m.text))
	v := records.Verified{
		Team: t.cfg.Team, ID: m.id, Item: m.kind, From: m.sender, Room: purpose, Text: m.text,
		BodySHA256: hex.EncodeToString(sum[:]), SentMS: m.ts, Human: m.kind == records.KindHuman,
	}
	if purpose == proto.PurposeQuestions {
		v.Question, v.Headline = t.questionOf(ctx, c, m, roomID, r, a)
	}
	return v
}

// questionOf is the question a questions-room message names, and its headline: a question is
// its own; this member's index answers for a human post it holds (`none` when attached to
// none); else the post's own relation, when it names a question this member can fetch (A 10.4
// rules 1 and 2); else null.
func (t *Team) questionOf(ctx context.Context, c *matrix.Client, m message, roomID string, r proto.Roster, a proto.Anchors) (*string, *string) {
	if m.content.Key == proto.KeyQuestion {
		return questionNamed(m.content.Question.N, m.text)
	}
	if q := t.qget(); q.idx != nil && q.room == roomID && m.kind == records.KindHuman {
		if n, seen := q.idx.Attachment(m.id); seen && n == 0 {
			return new("none"), nil
		} else if iq, ok := q.idx.Question(n); seen && ok {
			return new(fmt.Sprintf("Q%d", n)), new(iq.Headline)
		}
	}
	for _, rel := range []string{m.human.ThreadRoot, m.human.ReplyTo} {
		if rel == "" {
			continue
		}
		raw, err := c.GetEvent(ctx, roomID, rel)
		if err != nil {
			t.log.Printf("verify: the question %s names was not read: %s", m.id, matrix.Cause(err))
			continue
		}
		if root, ok := readRoomMessage(raw, r, a.Server, proto.PurposeQuestions); ok && root.content.Key == proto.KeyQuestion {
			return questionNamed(root.content.Question.N, root.text)
		}
	}
	return nil, nil
}

// questionNamed is `Q<n>` and the headline of a question whose body is text.
func questionNamed(n int64, text string) (*string, *string) {
	name := fmt.Sprintf("Q%d", n)
	h, err := proto.Headline(strings.TrimPrefix(text, name+" "))
	if err != nil {
		return &name, nil
	}
	return &name, &h
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
// event the server answers 404 or 403 for, one that does not parse, and a room this member
// does not read are team.ErrItemGone: the item can never be judged again. A questions item is
// fetched from the room the roster names and judged as that room's (QS6), or, by a member that
// does not coordinate, as a relayed post fetched by ID (A 10.6).
func (t *Team) Refetch(ctx context.Context, room, id string) (proto.Verdict, error) {
	c, pair, err := t.current()
	if err != nil {
		return proto.Verdict{}, err
	}
	s := &sess{t: t, c: c, pair: pair, a: anchorsOf(t.cfg.Team, pair)}
	if s.roster, err = t.rosterFor(ctx, c, s.a); err != nil {
		return proto.Verdict{}, err
	}
	roomID, res, err := t.refetchRoom(room, s.a, s.roster)
	if err != nil {
		return proto.Verdict{}, gone(err)
	}
	raw, err := t.getEvent(ctx, c, room, roomID, id, s.roster, s.a.Self)
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
	resolved, err := res.resolve(ctx, s.resolveInput(view, roomID), rels)
	if err != nil {
		return proto.Verdict{}, err
	}
	flood := proto.Window{Times: map[string][]int64{}}
	judge := proto.Judge{
		Server: s.a.Server, Self: s.a.Self, RoomID: roomID, Purpose: room, Roster: s.roster,
		NowMs: t.cfg.Clock.WallMS(), Lookup: &memLookup{view: view, resolved: resolved, judged: map[string]bool{}, skip: id}, Flood: &flood,
	}
	switch room {
	case proto.PurposeMain:
	case proto.PurposeQuestions:
		if s.roster.Sender(s.a.Self, s.a.Server) != proto.SenderCoordinator {
			// A member that does not coordinate holds a questions item only as a relayed post
			// (A 10.6): judged as fetched by ID, its relay account lost with the restart.
			return judge.Fetched(v), nil
		}
	default:
		return proto.Verdict{}, gone(exits.New(exits.CodeUsage, &t.cfg.Team, notARoom, exits.Name(room)))
	}
	vs := []proto.Verdict{t.judge(judge, v)}
	// The same check as a sync's (n3d-D12): a restored ping that no longer proves is a drop.
	news := t.forgeCheck(ctx, s.roster.Forge, vs)
	if err := ctx.Err(); err != nil {
		return proto.Verdict{}, err
	}
	t.noteForge(news)
	return vs[0], nil
}

// refetchRoom is the room a restored item of purpose room is fetched from, and the resolver of
// that room's relations (a room's own, as the sync's): main, or the questions room the roster
// names; any other room is refused.
func (t *Team) refetchRoom(room string, a proto.Anchors, r proto.Roster) (string, *resolver, error) {
	id, err := t.roomOf(room, a, r)
	if err != nil {
		return "", nil, err
	}
	switch room {
	case proto.PurposeMain:
		return id, t.res, nil
	case proto.PurposeQuestions:
		return id, t.qres, nil
	}
	return "", nil, exits.New(exits.CodeUsage, &t.cfg.Team, notARoom, exits.Name(room))
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
