package engine

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	msgReplyNoText    exits.Template = "reply reads its text on stdin, and none was given; or answer a ping with --verb"
	msgReplyVerbStdin exits.Template = "reply --verb reads no text"
	msgReplyRef       exits.Template = "--ref goes with --verb"
	msgAnswerTo       exits.Template = "an answer goes to its ping's sender: drop --to-coordinator"
	msgAnswerVerb     exits.Template = "reply --verb takes ack, nack, done or blocked, not {}"
	msgTimeoutReply   exits.Template = "a TIMEOUT is not a message; ping the member again"
	msgNotAnswerable  exits.Template = "{} is not a ping that expects an answer; reply without --verb"
	msgFormerMember   exits.Template = "the carried item's sender {} is not a member of team {}"
	msgOwnEvent       exits.Template = "{} is this member's own event"
	msgNoSuchItem     exits.Template = "team {} has no item {} in main"
	msgFetchFailed    exits.Template = "team {}'s homeserver did not answer for {}; try again later"
)

// replied is what a reply knows of the item it answers, from the store or from a fetch: its
// room, its sender (a carried item's original sender), and a ping's verb.
type replied struct {
	room, from string
	ping       bool
	verb       string
}

// reply is SD1 (n3d-D13): the team, room and addressee from the ledger where it holds the id,
// else from one fetch; a plain reply in main to the item's sender, or with --verb an answer in
// the ping's thread; then send's steps.
func (e *Engine) reply(ctx context.Context, a wire.ReplyArgs, stdin *string, out RecordWriter) wire.Done {
	text, err := replyText(a, stdin)
	if err != nil {
		return doneOf(err)
	}
	t, _, err := e.single(ctx, a.Team, a.ID)
	if err != nil {
		return doneOf(err)
	}
	st, err := e.replyTeam(ctx, t)
	if err != nil {
		return doneOf(err)
	}
	it, err := e.repliedTo(ctx, st, a.ID)
	if err != nil {
		return doneOf(err)
	}
	if it.room != proto.PurposeMain {
		return doneOf(exits.New(exits.CodeInternal, &t, msgUnbuilt, exits.Name("a reply in "+it.room)))
	}
	if a.Verb != nil {
		return e.answerPing(ctx, st, a, it, out)
	}
	to := []string{it.from}
	if a.ToCoordinator {
		c, ok := st.roster.Coordinator()
		if !ok {
			return doneOf(exits.New(exits.CodeUnknownMember, &t, msgNoCoordinator, exits.Team(t)))
		}
		if c != st.self && !slices.Contains(to, c) {
			to = append(to, c)
		}
	}
	return e.send(ctx, st, state.SendText, func(c proto.Composer) (proto.Outgoing, error) {
		return c.Text(proto.Text{To: to, Text: text}, a.ID, text)
	}, out)
}

// answerPing is reply --verb: an answer to a ping that expects one, to its sender.
func (e *Engine) answerPing(ctx context.Context, st sendTeam, a wire.ReplyArgs, it replied, out RecordWriter) wire.Done {
	if rule, known := proto.Verbs[it.verb]; !it.ping || !known || !rule.ExpectsAnswer {
		return doneOf(exits.New(exits.CodeRe, &st.id, msgNotAnswerable, exits.Name(a.ID)))
	}
	p := proto.Ping{Verb: *a.Verb, To: []string{it.from}}
	if a.Ref != nil {
		p.Ref = *a.Ref
	}
	body := pingBody(p, st.namer())
	return e.send(ctx, st, state.SendPing, func(c proto.Composer) (proto.Outgoing, error) {
		return c.Ping(p, a.ID, body)
	}, out)
}

// replyText is the checks a reply's arguments take before any team is read: a TIMEOUT's key
// (n3d-D26), --verb's own rules (n3d-D14, n3d-D15), and a plain reply's text.
func replyText(a wire.ReplyArgs, stdin *string) (string, error) {
	switch {
	case strings.Contains(a.ID, "~"):
		return "", exits.New(exits.CodeUsage, a.Team, msgTimeoutReply)
	case a.Verb == nil && a.Ref != nil:
		return "", exits.New(exits.CodeUsage, a.Team, msgReplyRef)
	case a.Verb == nil:
		return stdinText(a.Team, stdin, msgReplyNoText)
	case stdin != nil:
		return "", exits.New(exits.CodeUsage, a.Team, msgReplyVerbStdin)
	case a.ToCoordinator:
		return "", exits.New(exits.CodeUsage, a.Team, msgAnswerTo)
	case !proto.IsAnswerVerb(*a.Verb):
		return "", exits.New(exits.CodeVerb, a.Team, msgAnswerVerb, exits.Words(*a.Verb))
	}
	return "", nil
}

// replyTeam is team t as a send reads it: its goroutine, and its verified or cached roster.
func (e *Engine) replyTeam(ctx context.Context, t paths.TeamID) (sendTeam, error) {
	cands, err := e.candidates(ctx, &t)
	if err != nil {
		return sendTeam{}, err
	}
	c := cands[0]
	r, ok := c.roster()
	if !ok {
		return sendTeam{}, exits.New(exits.CodeUnreachable, &t, msgNoRoster, exits.Team(t))
	}
	return sendTeam{id: c.id, tm: c.tm, roster: r, self: c.self, anchors: c.anchors}, nil
}

// repliedTo is the item id as the store holds it, or fetched: an id in no ledger, and a notice
// (which may carry a v1 item), are read from the homeserver.
func (e *Engine) repliedTo(ctx context.Context, st sendTeam, id string) (replied, error) {
	var (
		stored state.Ledger
		known  bool
	)
	if err := e.do(ctx, func() {
		if h := e.teams[st.id]; h != nil && h.store != nil {
			stored, known = storedItem(h.store, id)
		}
	}); err != nil {
		return replied{}, exits.New(exits.CodeTimedOut, &st.id, msgTimedOut, exits.Team(st.id))
	}
	if known && stored.Item != records.KindNotice {
		it := replied{room: stored.Room, from: stored.From, ping: stored.Item == records.KindPing}
		if stored.Verb != nil {
			it.verb = *stored.Verb
		}
		return it, nil
	}
	return e.fetched(ctx, st, id)
}

// storedItem is id's ledger or pending entry.
func storedItem(s *state.Store, id string) (state.Ledger, bool) {
	if i := slices.IndexFunc(s.Ledger, func(l state.Ledger) bool { return l.ID == id }); i >= 0 {
		return s.Ledger[i], true
	}
	if i := slices.IndexFunc(s.Pending, func(p state.Pending) bool { return p.ID == id }); i >= 0 {
		return ledgerOfPending(s.Pending[i]), true
	}
	return state.Ledger{}, false
}

func ledgerOfPending(p state.Pending) state.Ledger {
	return state.Ledger{ID: p.ID, Room: p.Room, Item: p.Item, From: p.From, AtMS: p.SentMS, Verb: p.Verb}
}

// fetched is id judged again by its team (Team.Refetch, off the state goroutine): 23 for none
// or a verdict with no item, 64 for an own event, 20 for no answer; a carried item is its
// original sender's, who must still be a member (A 7.7).
func (e *Engine) fetched(ctx context.Context, st sendTeam, id string) (replied, error) {
	v, err := st.tm.Refetch(ctx, proto.PurposeMain, id)
	if err != nil {
		return replied{}, fetchErr(ctx, st.id, id, err)
	}
	switch {
	case v.Event.Sender == st.self:
		return replied{}, exits.New(exits.CodeUsage, &st.id, msgOwnEvent, exits.Name(id))
	case v.Outcome != proto.Deliver && v.Outcome != proto.Record:
		return replied{}, exits.New(exits.CodeNotFound, &st.id, msgNoSuchItem, exits.Team(st.id), exits.Name(id))
	case v.Carried:
		k := v.Content.Carried
		if _, member := st.roster.Members[k.From]; !member && !st.roster.IsHuman(k.From) {
			return replied{}, exits.New(exits.CodeTarget, &st.id, msgFormerMember, exits.Name(k.From), exits.Team(st.id))
		}
		return replied{room: proto.PurposeMain, from: k.From, ping: k.Kind == proto.CarriedPing, verb: k.Verb}, nil
	}
	return replied{room: proto.PurposeMain, from: v.Event.Sender, ping: v.Kind == proto.KindPing, verb: v.Content.Ping.Verb}, nil
}

// fetchErr is a failed fetch's exit: the team's own refusal, 23 for a 404, the request's end as
// timed-out, else 20 unreachable.
func fetchErr(ctx context.Context, t paths.TeamID, id string, err error) error {
	switch {
	case errors.Is(err, matrix.ErrNotFound):
		return exits.New(exits.CodeNotFound, &t, msgNoSuchItem, exits.Team(t), exits.Name(id))
	case ctx.Err() != nil:
		return exits.New(exits.CodeTimedOut, &t, msgTimedOut, exits.Team(t))
	}
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		return xe
	}
	return exits.New(exits.CodeUnreachable, &t, msgFetchFailed, exits.Team(t), exits.Name(id))
}
