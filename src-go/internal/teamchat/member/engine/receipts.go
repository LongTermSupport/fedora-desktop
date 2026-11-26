package engine

import (
	"context"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// PendingReceipts is t's read receipts not yet sent, and a channel closed when they next change
// (team.Receipts, B 2.7.5). A team not held has none, and a channel that never closes.
func (e *Engine) PendingReceipts(t paths.TeamID) ([]state.Receipt, <-chan struct{}) {
	var (
		rs      []state.Receipt
		changed <-chan struct{}
	)
	if err := e.do(context.Background(), func() {
		h := e.teams[t]
		if h == nil || h.removed {
			return
		}
		if h.store != nil {
			rs = slices.Clone(h.store.Receipts)
		}
		changed = h.receiptsChanged()
	}); err != nil {
		e.logf("team %s: the receipts were not read: %v", t, err)
	}
	return rs, changed
}

// ReceiptSent drops the receipt for id once the homeserver has it: one store write, none when
// the store holds no such receipt.
func (e *Engine) ReceiptSent(ctx context.Context, t paths.TeamID, id string) error {
	return e.change(ctx, t, func(_ *held, s *state.Store) (bool, error) {
		n := len(s.Receipts)
		s.Receipts = slices.DeleteFunc(s.Receipts, func(r state.Receipt) bool { return r.ID == id })
		return len(s.Receipts) != n, nil
	})
}

// PendingOwed is t's owed receipts not yet posted (Plan 00267 C2), and a channel closed when they
// or the read receipts next change. A team not held owes none, and a channel that never closes.
func (e *Engine) PendingOwed(t paths.TeamID) ([]state.Owed, <-chan struct{}) {
	var (
		owed    []state.Owed
		changed <-chan struct{}
	)
	if err := e.do(context.Background(), func() {
		h := e.teams[t]
		if h == nil || h.removed {
			return
		}
		if h.store != nil {
			owed = slices.Clone(h.store.Owed)
		}
		changed = h.receiptsChanged()
	}); err != nil {
		e.logf("team %s: the owed receipts were not read: %v", t, err)
	}
	return owed, changed
}

// OwedSent removes the owed receipt o once the homeserver has its post: one store write, none
// when the store does not owe it. A failed write leaves it owed in memory too.
func (e *Engine) OwedSent(ctx context.Context, t paths.TeamID, o state.Owed) error {
	return e.change(ctx, t, func(_ *held, s *state.Store) (bool, error) {
		return s.OwedSent(o), nil
	})
}

// receiptsChanged is the channel the next change of h's receipts (read or owed) closes; on the
// state goroutine.
func (h *held) receiptsChanged() <-chan struct{} {
	if h.receipts == nil {
		h.receipts = make(chan struct{})
	}
	return h.receipts
}

// receiptLists is a store's read and owed receipts, as a change compares them.
type receiptLists struct {
	read []state.Receipt
	owed []state.Owed
}

// receiptListsOf is s's receipt lists; none for no store.
func receiptListsOf(s *state.Store) receiptLists {
	if s == nil {
		return receiptLists{}
	}
	return receiptLists{read: s.Receipts, owed: s.Owed}
}

// tellReceipts closes the channel of h's receipts' waiters when before is not the store's
// receipts now, read or owed, or when the store is gone; on the state goroutine.
func (h *held) tellReceipts(before receiptLists) {
	if h.receipts == nil || h.store != nil && !h.removed &&
		slices.Equal(before.read, h.store.Receipts) && slices.Equal(before.owed, h.store.Owed) {
		return
	}
	close(h.receipts)
	h.receipts = nil
}
