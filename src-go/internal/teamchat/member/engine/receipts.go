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

// receiptsChanged is the channel the next change of h's receipts closes; on the state goroutine.
func (h *held) receiptsChanged() <-chan struct{} {
	if h.receipts == nil {
		h.receipts = make(chan struct{})
	}
	return h.receipts
}

// tellReceipts closes the channel of h's receipts' waiters when before is not the store's
// receipts now, or when the store is gone; on the state goroutine.
func (h *held) tellReceipts(before []state.Receipt) {
	if h.receipts == nil || h.store != nil && !h.removed && slices.Equal(before, h.store.Receipts) {
		return
	}
	close(h.receipts)
	h.receipts = nil
}
