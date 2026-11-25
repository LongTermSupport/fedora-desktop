package engine

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// threadAnswer is alice's post n in the questions room, in the thread of the question root,
// pending as the sync took it; the coordinator's index has attached it.
func threadAnswer(t *testing.T, f *fakeTeam, ops paths.TeamID, n int, root string, sent int64) team.Item {
	t.Helper()
	humanPost(t, f, ops, evID(n), "MIT", root, sent)
	return team.Item{
		Pending: state.Pending{ID: evID(n), Room: proto.PurposeQuestions, Item: records.KindHuman, SentMS: sent, From: alice(ops)},
		Verdict: proto.Verdict{
			Outcome: proto.Deliver, Kind: proto.KindHuman, Class: proto.SenderHuman,
			Event: proto.Event{ID: evID(n), OriginTs: sent}, Human: proto.Human{Body: "MIT", ThreadRoot: root},
		},
	}
}

// R34 (Plan 00265): a human's reply inside a question's thread is delivered with that question
// named, `question: Q1` and its headline, never `question: none`.
func TestR34AnInThreadReplyIsDeliveredWithItsQuestion(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := dw.coordinating(ops)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall)
	dw.persist(ops, threadAnswer(t, f, ops, 1, evID(qOwnEv), startWall+5))
	dw.facts(false)
	ev := dw.next()
	if !strings.Contains(ev.text, `question: Q1 "Which licence for the docs?"`) || strings.Contains(ev.text, "question: none") {
		t.Fatalf("the in-thread reply was delivered as %q, want question: Q1 and its headline", ev.text)
	}
}

// R34: recv gives the same reply's record its question and headline.
func TestR34AnInThreadReplysRecordNamesItsQuestion(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.coordinating(ops)
	countQuestion(t, f, ops, evID(qOwnEv), agent(ops, 1), startWall)
	w.persist(ops, threadAnswer(t, f, ops, 1, evID(qOwnEv), startWall+5))
	_, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if len(out.recs) != 1 {
		t.Fatalf("recv gave %s", out.kinds())
	}
	h, ok := out.recs[0].(records.Human)
	if !ok || h.Question == nil || *h.Question != "Q1" || h.Headline == nil || *h.Headline != "Which licence for the docs?" {
		t.Fatalf("the in-thread reply's record %+v, want question Q1 and its headline", out.recs[0])
	}
}
