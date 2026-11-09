package teamsync

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// gated runs tm with every long-poll held until the test lets it go, so
// what the test posts in between comes as one answer. The first answer (SY1) is taken.
func (w *world) gated(tm *Team) (*running, func()) {
	w.t.Helper()
	gate := make(chan struct{})
	tm.onPoll = func(string) {
		select {
		case <-gate:
		case <-w.t.Context().Done():
		}
	}
	r := w.startTeam(tm)
	w.batch()
	return r, func() {
		gate <- struct{}{}
		r.poll(w.t)
	}
}

func (w *world) peerTexts(n int) {
	w.t.Helper()
	for range n {
		w.agentText(peer, "flooding", user(me))
	}
}

// T19 (B 2.7.3, 15b): an agent's 11th PEER item within 600 s never enters pending and is one in
// the batch's HeldBack; the window slides on the clock, so the item after it is admitted; a
// human's items are never held back.
func TestAFloodingPeerIsHeldBack(t *testing.T) {
	w := newWorld(t)
	_, release := w.gated(w.newTeam())
	w.peerTexts(11)
	for range 11 {
		w.say(alice, human("from a human", user(me)))
	}
	release()
	b := w.batch()
	if len(b.Items) != 21 || b.HeldBack[user(peer)] != 1 || len(b.HeldBack) != 1 {
		t.Fatalf("items %d, held back %v", len(b.Items), b.HeldBack)
	}
	w.clk.StepWall(300_000)
	w.peerTexts(1)
	release()
	if b := w.batch(); len(b.Items) != 0 || b.HeldBack[user(peer)] != 1 {
		t.Fatalf("within the window: items %d, held back %v", len(b.Items), b.HeldBack)
	}
	w.clk.StepWall(300_000)
	w.peerTexts(1)
	release()
	if b := w.batch(); len(b.Items) != 1 || len(b.HeldBack) != 0 {
		t.Fatalf("past the window: items %d, held back %v", len(b.Items), b.HeldBack)
	}
}

// 15c: the window is committed only once the answer is persisted, as flood is, so an answer
// whose store write failed counts nothing, and judged again it admits the same items.
func TestADiscardedAnswerCountsNothingInTheBudget(t *testing.T) {
	w := newWorld(t)
	r, release := w.held(w.newTeam())
	failed := false
	w.eng.setPersistErr(func(team.Batch) error {
		if !failed {
			failed = true
			return errors.New("disk full")
		}
		return nil
	})
	w.peerTexts(10)
	release()
	w.clk.Advance(r.wait(t))
	if b := w.batch(); len(b.Items) != 10 || len(b.HeldBack) != 0 {
		t.Fatalf("items %d, held back %v", len(b.Items), b.HeldBack)
	}
}

// 15b, 15c: HUMAN, TIMEOUT, the admin's and carried items are never held back; a member's
// NOTICE and PING are, as PEER is.
func TestBudgeted(t *testing.T) {
	agentV := func(kind string) proto.Verdict {
		return proto.Verdict{Outcome: proto.Deliver, Kind: kind, Class: proto.SenderWorker}
	}
	carried := agentV(proto.KindNotice)
	carried.Carried = true
	admin := agentV(proto.KindNotice)
	admin.Class = proto.SenderAdmin
	human := agentV(proto.KindHuman)
	human.Class = proto.SenderHuman
	for name, c := range map[string]struct {
		v    proto.Verdict
		want bool
	}{
		"peer": {agentV(proto.KindPeer), true}, "ping": {agentV(proto.KindPing), true},
		"notice": {agentV(proto.KindNotice), true}, "timeout": {agentV(proto.KindTimeout), false},
		"carried": {carried, false}, "admin": {admin, false}, "human": {human, false},
	} {
		if got := budgeted(c.v); got != c.want {
			t.Errorf("%s: budgeted %v", name, got)
		}
	}
}

// The judging seam's verdicts: eleven carried NOTICEs from a worker are all admitted.
func TestCarriedItemsAreNeverHeldBack(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	tm.judge = func(j proto.Judge, v proto.Value) proto.Verdict {
		got := j.Judge(v)
		if got.Outcome == proto.Deliver {
			got.Kind, got.Carried = proto.KindNotice, true
		}
		return got
	}
	_, release := w.held(tm)
	w.peerTexts(11)
	release()
	if b := w.batch(); len(b.Items) != 11 || len(b.HeldBack) != 0 {
		t.Fatalf("items %d, held back %v", len(b.Items), b.HeldBack)
	}
}
