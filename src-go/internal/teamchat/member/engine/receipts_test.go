package engine

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// storeWithReceipts is a fresh store of id holding a receipt for each of ids.
func (w *world) storeWithReceipts(id paths.TeamID, ids ...string) []byte {
	s := w.newStore(id)
	for _, e := range ids {
		s.AddReceipt(state.Receipt{ID: e, Room: proto.PurposeMain})
	}
	return encodeStore(w.t, s)
}

// DV5: a receipt in the store is the team's after a link restart; ReceiptSent drops it in one
// store write and wakes the sender's wait.
func TestReceiptsAreKeptInTheStoreUntilSent(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithReceipts(ops, evID(1), evID(2)))
	<-f.running
	if f.cfg.Receipts == nil {
		t.Fatal("the team was not given the engine's receipts")
	}
	rs, changed := w.e.PendingReceipts(ops)
	if len(rs) != 2 || rs[0].ID != evID(1) {
		t.Fatalf("receipts %+v", rs)
	}
	check(t, w.e.ReceiptSent(t.Context(), ops, evID(1)))
	select {
	case <-changed:
	default:
		t.Fatal("the change was not told")
	}
	if left := w.store(ops).Receipts; len(left) != 1 || left[0].ID != evID(2) {
		t.Fatalf("the store holds %+v", left)
	}
	rs, changed = w.e.PendingReceipts(ops)
	check(t, w.e.ReceiptSent(t.Context(), ops, evID(9)))
	select {
	case <-changed:
		t.Fatal("a receipt not held changed the receipts")
	default:
	}
	if len(rs) != 1 {
		t.Fatalf("receipts %+v", rs)
	}
}

// DV5: a removed team's receipts go with its store; the sender's last call is ErrRemoved.
func TestARemovedTeamsReceiptsGo(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithReceipts(ops, evID(1)))
	<-f.running
	_, changed := w.e.PendingReceipts(ops)
	if _, err := w.e.Remove(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	<-changed
	if rs, never := w.e.PendingReceipts(ops); len(rs) != 0 || never != nil {
		t.Fatalf("receipts %+v, %v", rs, never)
	}
	if err := w.e.ReceiptSent(t.Context(), ops, evID(1)); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("ReceiptSent = %v", err)
	}
}

// owedTo is a bounced receipt owed to the sender of item n, a member of team t.
func owedTo(t paths.TeamID, n int) state.Owed {
	return state.Owed{Of: evID(n), To: agent(t, 2), State: proto.ReceiptBounced, Reason: proto.ReasonRate, AtMS: startWall}
}

// storeWithOwed is a fresh store of id owing each of owed.
func (w *world) storeWithOwed(id paths.TeamID, owed ...state.Owed) []byte {
	s := w.newStore(id)
	for _, o := range owed {
		check(w.t, s.AddOwed(o))
	}
	return encodeStore(w.t, s)
}

// Plan 00267 Task 2.3: an owed receipt is the team's after a link restart; OwedSent removes it in
// one store write and wakes the sender's wait, and one not owed changes nothing.
func TestOwedReceiptsAreKeptInTheStoreUntilSent(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithOwed(ops, owedTo(ops, 1), owedTo(ops, 2)))
	<-f.running
	owed, changed := w.e.PendingOwed(ops)
	if len(owed) != 2 || owed[0] != owedTo(ops, 1) {
		t.Fatalf("owed %+v", owed)
	}
	check(t, w.e.OwedSent(t.Context(), ops, owedTo(ops, 1)))
	select {
	case <-changed:
	default:
		t.Fatal("the change was not told")
	}
	if left := w.store(ops).Owed; len(left) != 1 || left[0] != owedTo(ops, 2) {
		t.Fatalf("the store owes %+v", left)
	}
	owed, changed = w.e.PendingOwed(ops)
	check(t, w.e.OwedSent(t.Context(), ops, owedTo(ops, 9)))
	select {
	case <-changed:
		t.Fatal("a receipt not owed changed the owed list")
	default:
	}
	if len(owed) != 1 {
		t.Fatalf("owed %+v", owed)
	}
}

// Plan 00267 Task 2.3 (the class of issue #443): an OwedSent whose store write fails moves
// nothing in memory, so the receipt is still owed and sent again.
func TestAFailedOwedSentKeepsTheReceipt(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithOwed(ops, owedTo(ops, 1)))
	<-f.running
	_, changed := w.e.PendingOwed(ops)
	check(t, w.root.RemoveTree(w.layout.Seat(ops).Dir()))
	if err := w.e.OwedSent(t.Context(), ops, owedTo(ops, 1)); err == nil {
		t.Fatal("OwedSent over a store that cannot be written succeeded")
	}
	select {
	case <-changed:
		t.Fatal("a failed write told a change")
	default:
	}
	if owed, _ := w.e.PendingOwed(ops); len(owed) != 1 || owed[0] != owedTo(ops, 1) {
		t.Fatalf("after a failed write the engine owes %+v", owed)
	}
}

// Plan 00267 Task 2.3: a bounce owed at a Persist wakes the sender's wait.
func TestABounceOwedAtAPersistWakesTheSender(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithOwed(ops))
	<-f.running
	_, changed := w.e.PendingOwed(ops)
	_, err := w.e.Persist(t.Context(), team.Batch{Team: ops, NextBatch: "s1", Bounces: []team.Bounce{{ID: evID(3), Room: proto.PurposeMain, From: agent(ops, 2), SentMS: 100, Reason: proto.ReasonRate}}})
	check(t, err)
	select {
	case <-changed:
	default:
		t.Fatal("the owed bounce was not told")
	}
	if owed, _ := w.e.PendingOwed(ops); len(owed) != 1 || owed[0].Of != evID(3) {
		t.Fatalf("owed %+v", owed)
	}
}

// Plan 00267 Task 2.3: a removed team owes nothing, and its sender's last call is ErrRemoved.
func TestARemovedTeamOwesNothing(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.add(ops, w.storeWithOwed(ops, owedTo(ops, 1)))
	<-f.running
	_, changed := w.e.PendingOwed(ops)
	if _, err := w.e.Remove(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	<-changed
	if owed, never := w.e.PendingOwed(ops); len(owed) != 0 || never != nil {
		t.Fatalf("owed %+v, %v", owed, never)
	}
	if err := w.e.OwedSent(t.Context(), ops, owedTo(ops, 1)); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("OwedSent = %v", err)
	}
}
