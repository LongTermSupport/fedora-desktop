package teamsync

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// relays are the relays the coordinator owes for a sync's fold of the questions room (A 10.6):
// each human post delivered from the room's floor on and attached to a question whose `asked_by`
// is not empty, to each seat it names, composed as the coordinator's text with `relay`. The
// build and the catch-up owe none, so a new coordinator relays only what it syncs. A seat the
// roster no longer reaches is passed over, logged.
func (s *sess) relays(jr roomJudged, idx *proto.QuestionIndex, floor int64) []team.Relay {
	var out []team.Relay
	for _, v := range jr.vs {
		if v.Outcome != proto.Deliver || v.Kind != proto.KindHuman || v.Event.OriginTs <= floor {
			continue
		}
		n, seen := idx.Attachment(v.Event.ID)
		q, ok := idx.Question(n)
		if !seen || !ok {
			continue
		}
		for _, seat := range q.AskedBy {
			r, err := s.relay(v.Event.ID, seat, n)
			if err != nil {
				s.t.log.Printf("the answer %s on Q%d is not relayed to %s: %v", v.Event.ID, n, seat, err)
				continue
			}
			out = append(out, r)
		}
	}
	return out
}

// relay is the relay of post, attached to Q<n>, to seat.
func (s *sess) relay(post, seat string, n int64) (team.Relay, error) {
	c := proto.Composer{Server: s.a.Server, Self: s.a.Self, Token: s.pair.Token.Reveal(), Roster: s.roster}
	text := proto.Text{To: []string{seat}, Text: proto.RelayText(n), Relay: &proto.Relay{Room: proto.PurposeQuestions, ID: post, Q: n}}
	out, err := c.Text(text, "", text.Text)
	if err != nil {
		return team.Relay{}, err
	}
	return team.Relay{Txn: proto.RelayTxn(post, seat), Content: out.Content.Compact()}, nil
}

// isRelay is a delivered text with `relay` (A 7.3): the relay path's, never a PEER item.
func isRelay(v proto.Verdict) bool {
	return v.Outcome == proto.Deliver && v.Content.Key == proto.KeyText && v.Content.Text.Relay != nil
}

// relayed is A 10.6's relay check of a delivered relay: the post fetched from questions by this
// member's link and judged there, so the item is the human's own words, or the relay dropped
// `provenance`. A transient failure of the fetch is the error, and the answer is judged again.
func (s *sess) relayed(ctx context.Context, j *judged, v proto.Verdict) (proto.Verdict, error) {
	t := s.t
	room := s.roster.Rooms.Questions
	if room == "" {
		return proto.RelayDropped(v), nil
	}
	id := v.Content.Text.Relay.ID
	raw, err := t.getEvent(ctx, s.c, proto.PurposeQuestions, room, id, s.roster, s.a.Self)
	switch {
	case errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden):
		t.log.Printf("the post %s a relay names was not read: %s", id, matrix.Cause(err))
		return proto.RelayDropped(v), nil
	case err != nil:
		return proto.Verdict{}, err
	}
	post, ok := parseFetched(raw)
	if !ok {
		return proto.RelayDropped(v), nil
	}
	lk, err := s.fetchedLookup(ctx, room, id, post)
	if err != nil {
		return proto.Verdict{}, err
	}
	judge := proto.Judge{
		Server: s.a.Server, Self: s.a.Self, RoomID: room, Purpose: proto.PurposeQuestions,
		Roster: s.roster, NowMs: j.now, Lookup: lk, Flood: &j.flood,
	}
	return judge.Relayed(v, post), nil
}

// fetchedLookup is the Lookup a questions post fetched by ID is judged over: its relations
// resolved as a sync's are, through the questions room's resolver.
func (s *sess) fetchedLookup(ctx context.Context, room, id string, post proto.Value) (*memLookup, error) {
	t := s.t
	relIDs := proto.RelationIDs(post, s.roster)
	view, err := t.cfg.Engine.View(t.cfg.Team, []string{id}, relIDs, s.roster)
	if err != nil {
		return nil, err
	}
	rels := make([]relation, 0, len(relIDs))
	for _, rid := range relIDs {
		rels = append(rels, relation{event: id, id: rid})
	}
	resolved, err := t.qres.resolve(ctx, s.resolveInput(view, room), rels)
	if err != nil {
		return nil, err
	}
	return &memLookup{view: view, resolved: resolved, judged: map[string]bool{}, skip: id}, nil
}

// parseFetched is a fetched event as a Value; false for one that does not parse.
func parseFetched(raw []byte) (proto.Value, bool) {
	v, err := proto.Parse(raw)
	return v, err == nil
}
