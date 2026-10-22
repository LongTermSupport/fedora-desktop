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
		check(w.t, s.AddReceipt(state.Receipt{ID: e, Room: proto.PurposeMain}))
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
