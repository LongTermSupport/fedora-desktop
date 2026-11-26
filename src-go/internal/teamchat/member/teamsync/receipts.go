package teamsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// startReceipts runs B 2.7.5's receipt sender and the owed receipts' sender (Plan 00267 C2)
// beside the session's long-poll, on its client, and is the stop that ends them and waits for
// them. A stopped team runs no session, so it sends no receipt; nil Receipts sends none.
func (s *sess) startReceipts(ctx context.Context) func() {
	if s.t.cfg.Receipts == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { s.receipts(ctx) })
	wg.Go(func() { s.owed(ctx) })
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
		t.log.Printf("receipts wait for the next session: %s", matrix.Cause(err))
		return receiptEnd
	case never:
		t.log.Printf("receipt for %s dropped: the roster names no %s room (verified %t)", r.ID, r.Room, verified)
	case errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden):
		t.log.Printf("receipt for %s dropped: it can never be sent (%s)", r.ID, matrix.Cause(err))
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

// owed posts each receipt the engine owes as A 7.8's `teamchat.receipt` in the reports room
// (Plan 00267 C2, decision D1), then reports it posted. Nothing is ever dropped: any failure
// keeps it and waits the team's backoff, and a refused token waits for the next session.
func (s *sess) owed(ctx context.Context) {
	t := s.t
	delay := time.Duration(0)
	for {
		owed, changed := t.cfg.Receipts.PendingOwed(t.cfg.Team)
		retry := false
	send:
		for _, o := range owed {
			switch s.postOwed(ctx, o) {
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

// postOwed posts o once, under the transaction ID derived from it, so a post that reached the
// homeserver and is made again (its record failed, the link restarted) is one event.
func (s *sess) postOwed(ctx context.Context, o state.Owed) receiptOutcome {
	t := s.t
	err := s.sendOwed(ctx, o)
	switch {
	case ctx.Err() != nil:
		return receiptEnd
	case errors.Is(err, matrix.ErrAuthRefused):
		t.log.Printf("owed receipts wait for the next session: %s", matrix.Cause(err))
		return receiptEnd
	case err != nil:
		t.log.Printf("receipt for %s kept, posted again after a backoff: %v", o.Of, err)
		return receiptRetry
	}
	switch err := t.cfg.Receipts.OwedSent(ctx, t.cfg.Team, o); {
	case err == nil:
		return receiptSent
	case errors.Is(err, team.ErrRemoved):
		t.log.Printf("owed receipts end: the team was removed")
		return receiptEnd
	case ctx.Err() != nil:
		return receiptEnd
	default:
		t.log.Printf("the posted receipt for %s was not recorded; it is posted again: %v", o.Of, err)
		return receiptRetry
	}
}

// errReportsNotJoined is an owed receipt whose reports room this member has not joined yet.
var errReportsNotJoined = errors.New("teamsync: the reports room is not joined yet")

// errNoReportsRoom is an owed receipt with no reports room in the verified roster to go to.
var errNoReportsRoom = errors.New("teamsync: the verified roster names no reports room")

// sendOwed is one post of o in the reports room.
func (s *sess) sendOwed(ctx context.Context, o state.Owed) error {
	return s.t.sendOwed(ctx, s.c, o)
}

// FlushOwed posts each receipt of owed once in the reports room, at a removal before the store
// goes (Plan 00267 C2), under the owed sender's transaction IDs, so one it posted already lands
// once; each is tried, and the failures are the error.
func (t *Team) FlushOwed(ctx context.Context, owed []state.Owed) error {
	c, _, err := t.current()
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range owed {
		if err := t.sendOwed(ctx, c, o); err != nil {
			errs = append(errs, fmt.Errorf("receipt for %s: %w", o.Of, err))
		}
	}
	return errors.Join(errs...)
}

// sendOwed is one post of o in the reports room, on the client c.
func (t *Team) sendOwed(ctx context.Context, c *matrix.Client, o state.Owed) error {
	roster, verified := t.Roster()
	switch {
	case !verified || roster.Rooms.Reports == "":
		return errNoReportsRoom
	case t.ReportsRoom().State != team.ReportsJoined:
		return errReportsNotJoined
	}
	content, err := proto.ReceiptContent(proto.Receipt{Of: o.Of, To: []string{o.To}, State: o.State, Reason: o.Reason, At: o.AtMS})
	if err != nil {
		return err
	}
	_, err = c.Send(ctx, roster.Rooms.Reports, proto.MessageType, proto.ReceiptTxn(o.Of, o.To, o.State, o.Reason), content.Compact())
	return err
}
