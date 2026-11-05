package teamsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
)

// startReceipts runs B 2.7.5's receipt sender beside the session's long-poll, on its client, and
// is the stop that ends it and waits for it. A stopped team runs no session, so it sends no
// receipt; nil Receipts sends none.
func (s *sess) startReceipts(ctx context.Context) func() {
	if s.t.cfg.Receipts == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { s.receipts(ctx) })
	return func() {
		cancel()
		wg.Wait()
	}
}

// The outcomes of one receipt.
type receiptOutcome int

const (
	receiptSent  receiptOutcome = iota // sent, or dropped as never sendable
	receiptRetry                       // kept, for a backoff
	receiptEnd                         // the sender ends with the session
)

// receipts sends each receipt the engine holds as m.read, then reports it sent. A transient
// failure keeps it and waits the team's backoff (SY8's numbers); a refused token waits for the
// next session; a receipt the homeserver can never take is dropped with one line.
func (s *sess) receipts(ctx context.Context) {
	t := s.t
	delay := time.Duration(0)
	for {
		rs, changed := t.cfg.Receipts.PendingReceipts(t.cfg.Team)
		retry := false
	send:
		for _, r := range rs {
			switch s.receipt(ctx, r) {
			case receiptEnd:
				return
			case receiptRetry:
				retry = true
				break send
			case receiptSent:
				delay = 0
			}
		}
		if !s.waitReceipts(ctx, changed, retry, &delay) {
			return
		}
	}
}

// waitReceipts waits for the receipts to change or, after a failure, for the next backoff; false
// when ctx ended.
func (s *sess) waitReceipts(ctx context.Context, changed <-chan struct{}, retry bool, delay *time.Duration) bool {
	t := s.t
	var wake <-chan time.Time
	if retry {
		*delay = min(max(2**delay, firstBackoff), maxBackoff)
		d := *delay - t.cfg.Jitter(*delay)
		ch, stop := t.cfg.Clock.Hold(d)
		defer stop()
		t.onReceiptWait(d)
		wake = ch
	}
	select {
	case <-changed:
	case <-wake:
	case <-ctx.Done():
		return false
	}
	return true
}

// receipt sends r once.
func (s *sess) receipt(ctx context.Context, r state.Receipt) receiptOutcome {
	t := s.t
	roster, verified := t.Roster()
	room, err := t.roomOf(r.Room, s.a, roster)
	never := err != nil
	if !never {
		err = s.c.ReadReceipt(ctx, room, r.ID)
	}
	switch {
	case ctx.Err() != nil:
		return receiptEnd
	case errors.Is(err, matrix.ErrAuthRefused):
		t.log.Printf("receipts wait for the next session: %s", matrix.Class(err))
		return receiptEnd
	case never:
		t.log.Printf("receipt for %s dropped: the roster names no %s room (verified %t)", r.ID, r.Room, verified)
	case errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden):
		t.log.Printf("receipt for %s dropped: it can never be sent (%s)", r.ID, matrix.Class(err))
	case err != nil:
		return receiptRetry
	}
	switch err := t.cfg.Receipts.ReceiptSent(ctx, t.cfg.Team, r.ID); {
	case err == nil:
		return receiptSent
	case errors.Is(err, team.ErrRemoved):
		t.log.Printf("receipts end: the team was removed")
		return receiptEnd
	case ctx.Err() != nil:
		return receiptEnd
	default:
		t.log.Printf("the sent receipt was not recorded; it is sent again: %v", err)
		return receiptRetry
	}
}
