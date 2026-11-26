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

// T19 (B 2.7.3, 15b), Plan 00267 C4: an agent's 11th PEER item within 600 s enters pending held
// to the budget window's next free slot, never dropped and never a HeldBack; the slots reserved
// count, so the next is held to the same slot; past the window an item is not held; a human's
// items are never held.
func TestAFloodingPeerIsHeldToTheNextFreeSlot(t *testing.T) {
	w := newWorld(t)
	_, release := w.gated(w.newTeam())
	now := w.clk.WallMS()
	w.peerTexts(11)
	for range 11 {
		w.say(alice, human("from a human", user(me)))
	}
	release()
	b := w.batch()
	if len(b.Items) != 22 || len(b.HeldBack) != 0 {
		t.Fatalf("items %d, held back %v", len(b.Items), b.HeldBack)
	}
	var held []int64
	for _, it := range b.Items {
		if it.Pending.NotBefore != 0 {
			held = append(held, it.Pending.NotBefore)
			if it.Held != proto.ReasonBudget {
				t.Errorf("an item held by the budget gives %q as its held reason", it.Held)
			}
		} else if it.Held != "" {
			t.Errorf("an item not held gives %q as its held reason", it.Held)
		}
	}
	if len(held) != 1 || held[0] != now+proto.DeliveryWindowMs {
		t.Fatalf("held to %v, want one item to %d", held, now+proto.DeliveryWindowMs)
	}
	w.clk.StepWall(300_000)
	w.peerTexts(1)
	release()
	if b := w.batch(); len(b.Items) != 1 || b.Items[0].Pending.NotBefore != now+proto.DeliveryWindowMs {
		t.Fatalf("within the window: %+v", b.Items)
	}
	w.clk.StepWall(300_000)
	w.peerTexts(1)
	release()
	if b := w.batch(); len(b.Items) != 1 || b.Items[0].Pending.NotBefore != 0 || len(b.HeldBack) != 0 {
		t.Fatalf("past the window: %+v, held back %v", b.Items, b.HeldBack)
	}
}

// Plan 00267 Task 4.1: an item's held reason is its forge check still to run, else whichever of
// the receive window and the delivery budget holds it the later; none for an item not held.
func TestHeldWhy(t *testing.T) {
	const now = 1000
	for _, tc := range []struct {
		rate, budget int64
		forge, want  string
	}{
		{0, 0, "", ""},
		{now, now, "", ""},
		{now + 5, 0, "", proto.ReasonRate},
		{0, now + 5, "", proto.ReasonBudget},
		{now + 9, now + 5, "", proto.ReasonRate},
		{now + 5, now + 9, "", proto.ReasonBudget},
		{now + 5, now + 5, "", proto.ReasonRate},
		{0, 0, proto.ReasonUnresolved, proto.ReasonUnresolved},
		{now + 5, now + 9, proto.ReasonForgeRate, proto.ReasonForgeRate},
	} {
		if got := heldWhy(tc.rate, tc.budget, now, tc.forge); got != tc.want {
			t.Errorf("heldWhy(%d, %d, %d, %q) = %q, want %q", tc.rate, tc.budget, now, tc.forge, got, tc.want)
		}
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
