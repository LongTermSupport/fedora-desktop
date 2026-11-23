package teamsync

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	msgQNotReady  exits.Template = "the questions room of team {} is not ready (joining, building its index, or waiting for the echo of an earlier post); try again shortly"
	msgQRole      exits.Template = "this member is not the coordinator of team {}, so it posts nothing in questions; ask the coordinator with teamchat say --to-coordinator --team {}"
	msgQUntrusted exits.Template = "the questions room of team {} fails its check {}; tell a human"
	msgQNotInRoom exits.Template = "event {} is not in the team's questions room"
)

// errQMoved is a compose whose room or echo state moved between the wait and the hold; the
// wait is made again.
var errQMoved = errors.New("teamsync: the questions room moved before the compose")

// qown is what this run knows of its own questions posts (QX3): whether the sends still in
// `sending` are placed on the live index, and the posts this run made, by event id, whose
// echo a compose need not wait for.
type qown struct {
	seeded bool
	posted map[string]bool
}

// qsignal wakes every compose waiting on the room; under q.mu.
func (q *qroom) qsignal() {
	if q.changed != nil {
		close(q.changed)
	}
	q.changed = make(chan struct{})
}

// qwatch is the room's state and a channel closed at its next change.
func (t *Team) qwatch() (qstate, <-chan struct{}) {
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	if t.q.changed == nil {
		t.q.changed = make(chan struct{})
	}
	return t.q.s, t.q.changed
}

// Questions is team.Team's: QX4's one compose at a time, under the judging hold.
func (t *Team) Questions(ctx context.Context, compose func(team.QuestionsView) ([]team.Placement, error)) (team.QuestionsView, error) {
	for {
		q, changed := t.qwatch()
		switch {
		case q.room == "":
			return team.QuestionsView{}, exits.New(exits.CodeRole, &t.cfg.Team, msgQRole, exits.Team(t.cfg.Team), exits.Team(t.cfg.Team))
		case q.check != "":
			return team.QuestionsView{}, exits.New(exits.CodeUntrusted, &t.cfg.Team, msgQUntrusted, exits.Team(t.cfg.Team), exits.Words(q.check))
		case q.ready():
			v, err := t.qcompose(ctx, compose)
			if !errors.Is(err, errQMoved) {
				return v, err
			}
		}
		if t.onQWait != nil {
			t.onQWait()
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return team.QuestionsView{}, exits.New(exits.CodeNotReady, &t.cfg.Team, msgQNotReady, exits.Team(t.cfg.Team))
		}
	}
}

// qcompose is one compose under the judging hold: the room still ready, no earlier run's post
// unechoed, the queued sends placed, then compose on a copy and its placements made live.
func (t *Team) qcompose(ctx context.Context, compose func(team.QuestionsView) ([]team.Placement, error)) (team.QuestionsView, error) {
	var out team.QuestionsView
	err := t.judgingHold(ctx, func() error {
		self, store, err := t.qstore()
		if err != nil {
			return err
		}
		q := t.qget()
		if !q.ready() || t.qwaitsEcho(store) {
			return errQMoved
		}
		idx := q.idx.Copy()
		if !q.own.seeded {
			t.qplace(idx, self, sendingPlacements(store))
		}
		ps, err := compose(team.QuestionsView{Room: q.room, Index: idx.Copy()})
		if err != nil {
			return err
		}
		t.qplace(idx, self, ps)
		if !t.qset(q.room, func(s *qstate) { s.idx, s.own.seeded = idx, true }) {
			t.log.Printf("the questions room changed during a compose; its post is placed at the next one")
		}
		out = team.QuestionsView{Room: q.room, Index: idx.Copy()}
		return nil
	})
	return out, err
}

// qstore is this member's user id and its team's store.
func (t *Team) qstore() (string, state.Store, error) {
	_, pair, err := t.current()
	if err != nil {
		return "", state.Store{}, err
	}
	self := anchorsOf(t.cfg.Team, pair).Self
	store, err := t.cfg.Engine.Load(t.cfg.Team, self)
	return self, store, err
}

// qwaitsEcho is QX3's wait: an own post is posted and its echo not folded, and this run did not
// make it, so no placement holds its number.
func (t *Team) qwaitsEcho(store state.Store) bool {
	u := store.Outbox.QuestionsUnechoed
	if u == nil {
		return false
	}
	q := t.qget()
	return !q.own.posted[*u]
}

// qplace places each own post on idx (QX3); one that cannot be placed is logged by its
// transaction, and its send is still posted, judged again by the sender's check.
func (t *Team) qplace(idx *proto.QuestionIndex, self string, ps []team.Placement) {
	for _, p := range ps {
		var err error
		switch {
		case p.Question != nil:
			err = idx.PlaceQuestion(p.Txn, self, *p.Question, p.Body)
		case p.Thread != nil:
			err = idx.PlaceThread(p.Txn, self, *p.Thread)
		}
		if err != nil {
			t.log.Printf("the questions post %s is not placed in the index: %v", p.Txn, err)
		}
	}
}

// sendingPlacements are the placements of the questions sends still in `sending`, in order
// (QX3 after a restart): questions and thread posts; a link-back and a retitle place nothing.
func sendingPlacements(store state.Store) []team.Placement {
	var ps []team.Placement
	for _, x := range store.Outbox.Sending {
		if x.Room != proto.PurposeQuestions {
			continue
		}
		if p, ok := placementOf(x.Txn, x.Content); ok {
			ps = append(ps, p)
		}
	}
	return ps
}

// placementOf is the placement of a questions content, false for one that places nothing.
func placementOf(txn string, content json.RawMessage) (team.Placement, bool) {
	v, err := proto.Parse(content)
	if err != nil {
		return team.Placement{}, false
	}
	dc, reason := proto.DecodeContent(v, proto.QuestionKeys)
	switch {
	case reason != "":
		return team.Placement{}, false
	case dc.Key == proto.KeyQuestion:
		q := dc.Question
		return team.Placement{Txn: txn, Question: &q, Body: dc.Body}, true
	case dc.Key == proto.KeyThread && dc.Thread.Link == "":
		th := proto.PlacedThread{N: dc.Thread.N, Close: dc.Thread.Close}
		if th.N == 0 {
			th.ReplyTo = dc.Relation.InReplyTo
		}
		return team.Placement{Txn: txn, Thread: &th}, true
	}
	return team.Placement{}, false
}

// QuestionPosted is team.Team's: the post id is this run's, so a compose does not wait for it.
func (t *Team) QuestionPosted(_, id string) {
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	if t.q.s.own.posted == nil {
		t.q.s.own.posted = map[string]bool{}
	}
	t.q.s.own.posted[id] = true
	t.q.qsignal()
}

// QuestionsPostable is team.Team's: the sender's check before a queued questions post is put.
func (t *Team) QuestionsPostable(content proto.Value) error {
	q := t.qget()
	if !q.ready() {
		return team.ErrQuestionsNotReady
	}
	dc, reason := proto.DecodeContent(content, proto.QuestionKeys)
	if reason != "" || dc.Key != proto.KeyQuestion {
		return nil
	}
	if held, ok := q.idx.Question(dc.Question.N); ok && !held.Provisional {
		return &proto.RefusalError{Reason: proto.ReasonTarget, Detail: proto.QuestionName(int(dc.Question.N)) + " is another question now"}
	}
	return nil
}

// FetchEvent is team.Team's: one event of the questions room, by id.
func (t *Team) FetchEvent(ctx context.Context, id string) (json.RawMessage, error) {
	q := t.qget()
	if q.room == "" {
		return nil, exits.New(exits.CodeRole, &t.cfg.Team, msgQRole, exits.Team(t.cfg.Team), exits.Team(t.cfg.Team))
	}
	c, _, err := t.current()
	if err != nil {
		return nil, err
	}
	raw, err := c.GetEvent(ctx, q.room, id)
	if errors.Is(err, matrix.ErrNotFound) {
		return nil, exits.New(exits.CodeNotFound, &t.cfg.Team, msgQNotInRoom, exits.Name(id))
	}
	return raw, t.verbErr(err)
}
