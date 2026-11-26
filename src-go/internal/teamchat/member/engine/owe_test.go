package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/faketranscript"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// owedLines are s's owed receipts as "<of n> <to> <state> <reason>", in queue order, the time
// left out (a proof world's clock moves).
func owedLines(s state.Store) []string {
	out := make([]string, 0, len(s.Owed))
	for _, o := range s.Owed {
		out = append(out, fmt.Sprintf("%s %s %s %s", o.Of, o.To, o.State, o.Reason))
	}
	return out
}

func owedLine(n int, to, st, reason string) string {
	return fmt.Sprintf("%s %s %s %s", evID(n), to, st, reason)
}

func checkOwed(t *testing.T, s state.Store, want ...string) {
	t.Helper()
	if got := owedLines(s); !slices.Equal(got, want) {
		t.Fatalf("owed\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// fullOwed is a store's owed queue filled to its cap with receipts of other events.
func fullOwed(id paths.TeamID) []state.Owed {
	out := make([]state.Owed, 0, 256)
	for i := range 256 {
		out = append(out, owedTo(id, 10_000+i))
	}
	return out
}

// Plan 00267 Task 4.1: who a receipt goes to. A PEER or PING item's sender is owed every state;
// a HUMAN's only a bounce (its m.read says delivered); a TIMEOUT is this member's own clock and
// a NOTICE the admin's, owed none, and nothing is owed to this member itself. A terminal state
// replaces a held one still queued for the item and sender; a held one replaces nothing.
func TestOweGoesToAMemberOrHumanSenderOnly(t *testing.T) {
	ops := tid(t, "ops")
	self := agent(ops, 1)
	for _, tc := range []struct {
		kind, from, state string
		owed              bool
	}{
		{records.KindPeer, agent(ops, 2), proto.ReceiptDelivered, true},
		{records.KindPing, agent(ops, 2), proto.ReceiptHeld, true},
		{records.KindPing, agent(ops, 2), proto.ReceiptRecorded, true},
		{records.KindPeer, agent(ops, 2), proto.ReceiptBounced, true},
		{records.KindHuman, alice(ops), proto.ReceiptBounced, true},
		{records.KindHuman, alice(ops), proto.ReceiptDelivered, false},
		{records.KindHuman, alice(ops), proto.ReceiptHeld, false},
		{records.KindTimeout, agent(ops, 2), proto.ReceiptBounced, false},
		{records.KindNotice, "@admin:" + paths.ServerName(ops), proto.ReceiptBounced, false},
		{records.KindPeer, self, proto.ReceiptDelivered, false},
		{records.KindPeer, "", proto.ReceiptDelivered, false},
	} {
		s := state.NewStore(ops, self, state.StoreRun{StartedMS: startWall}, state.Gate{})
		p := state.Pending{ID: evID(1), Room: proto.PurposeMain, Item: tc.kind, SentMS: 1, From: tc.from}
		reason := ""
		if tc.state == proto.ReceiptHeld {
			reason = proto.ReasonRate
		}
		if tc.state == proto.ReceiptBounced {
			reason = proto.ReasonRender
		}
		check(t, owe(&s, p, tc.state, reason, startWall))
		if got := len(s.Owed) == 1; got != tc.owed {
			t.Errorf("%s from %q, %s: owed %+v, want owed %t", tc.kind, tc.from, tc.state, s.Owed, tc.owed)
		}
	}
	s := state.NewStore(ops, self, state.StoreRun{StartedMS: startWall}, state.Gate{})
	p := peerItem(ops, 1, 1, "x").Pending
	check(t, owe(&s, p, proto.ReceiptHeld, proto.ReasonAbsent, startWall))
	check(t, owe(&s, p, proto.ReceiptHeld, proto.ReasonMissing, startWall))
	checkOwed(t, s, owedLine(1, agent(ops, 2), proto.ReceiptHeld, proto.ReasonAbsent), owedLine(1, agent(ops, 2), proto.ReceiptHeld, proto.ReasonMissing))
	check(t, owe(&s, p, proto.ReceiptDelivered, "", startWall))
	checkOwed(t, s, owedLine(1, agent(ops, 2), proto.ReceiptDelivered, ""))
}

// Plan 00267 Task 4.1: an item added while delivery is down (a socket session not live) owes
// its sender held(absent), and its proof then owes delivered, which replaces the held receipt
// while still queued. A HUMAN item gets its m.read and no owed receipt.
func TestAProvenDeliveryOwesDeliveredAndAnAbsentSessionHeld(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.persist(a, humanItem(a, 1, startWall, "hello"), peerItem(a, 2, startWall+1, "peer"))
	checkOwed(t, pw.store(a), owedLine(2, agent(a, 2), proto.ReceiptHeld, proto.ReasonAbsent))
	pw.facts(true)
	ev := pw.next()
	pw.until(func() bool { return writtenOf(pw.e.teams[a].store, nonceOf(t, ev.text)) == state.WrittenYes })
	checkOwed(t, pw.store(a), owedLine(2, agent(a, 2), proto.ReceiptHeld, proto.ReasonAbsent))
	pw.tr.Record(ev.text, faketranscript.Idle, 0)
	pw.settled(a)
	checkOwed(t, pw.store(a), owedLine(2, agent(a, 2), proto.ReceiptDelivered, ""))
	if pw.store(a).Owed[0].AtMS != pw.clk.WallMS() {
		t.Fatalf("delivered at %d, want the proof's time %d", pw.store(a).Owed[0].AtMS, pw.clk.WallMS())
	}
}

// Plan 00267 Task 4.1: an item added while delivery is live owes nothing until its proof; a
// link not in socket mode owes no held(absent), as recv is its way of delivery.
func TestOnlyASocketSessionNotLiveOwesHeldAbsent(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		pw := newProofWorld(t)
		a := tid(t, "alpha")
		pw.live(a)
		pw.facts(true)
		pw.persist(a, peerItem(a, 2, startWall+1, "peer"))
		checkOwed(t, pw.store(a))
	})
	t.Run("not socket mode", func(t *testing.T) {
		w := newWorld(t, state.DeliverySocket)
		ops := tid(t, "ops")
		w.held(ops)
		w.persist(ops, peerItem(ops, 2, 1, "x"))
		checkOwed(t, w.store(ops))
	})
}

// Plan 00267 Task 4.1: a delivery declared missing owes held(missing) for each item; its late
// find owes delivered, which replaces it.
func TestAMissingDeliveryOwesHeldThenItsLateFindDelivered(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.facts(true)
	text := pw.deliver(a, true, peerItem(a, 1, startWall, "one"), peerItem(a, 2, startWall+1, "two"))
	pw.quietFor(quietAfter)
	checkMissing(t, pw.store(a), nonceOf(t, text))
	checkOwed(t, pw.store(a),
		owedLine(1, agent(a, 2), proto.ReceiptHeld, proto.ReasonMissing), owedLine(2, agent(a, 2), proto.ReceiptHeld, proto.ReasonMissing))
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.until(func() bool { return len(pw.e.teams[a].store.Missed) == 0 })
	checkOwed(t, pw.store(a),
		owedLine(1, agent(a, 2), proto.ReceiptDelivered, ""), owedLine(2, agent(a, 2), proto.ReceiptDelivered, ""))
}

// Plan 00267 Task 4.1: recv plus its ack owes delivered; a recv returned owes nothing.
func TestAnAckedRecvOwesDelivered(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, peerItem(ops, 1, 1, "x"))
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	check(t, w.e.Return(t.Context(), d.Ack))
	checkOwed(t, w.store(ops))
	d = w.done(wire.OpRecv, wire.TeamsArgs{})
	checkOwed(t, w.store(ops))
	check(t, w.e.Ack(t.Context(), d.Ack))
	checkOwed(t, w.store(ops), owedLine(1, agent(ops, 2), proto.ReceiptDelivered, ""))
}

// Plan 00267 issue #443's class: an ack whose delivered receipt the full queue refuses is not
// settled, on disk or in memory; once the queue has room, the ack settles it.
func TestARefusedDeliveredReceiptLeavesTheAckUnsettled(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Owed = fullOwed(ops)
	w.heldWith(ops, s)
	w.persist(ops, peerItem(ops, 1, 1, "x"))
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	if err := w.e.Ack(t.Context(), d.Ack); !errors.Is(err, state.ErrFull) {
		t.Fatalf("an ack whose receipt is refused = %v, want the queue's refusal", err)
	}
	if p := w.store(ops).Pending; len(p) != 1 || p[0].Handed == nil {
		t.Fatalf("pending on disk %+v", p)
	}
	w.local(func() {
		if p := w.e.teams[ops].store.Pending; len(p) != 1 || p[0].Handed == nil {
			t.Errorf("pending in memory %+v", p)
		}
	})
	check(t, w.e.OwedSent(t.Context(), ops, owedTo(ops, 10_000)))
	check(t, w.e.Ack(t.Context(), d.Ack))
	if s := w.store(ops); len(s.Pending) != 0 || !slices.Contains(owedLines(s), owedLine(1, agent(ops, 2), proto.ReceiptDelivered, "")) {
		t.Fatalf("after the queue had room: pending %+v, owed %v", s.Pending, owedLines(s))
	}
}

// Plan 00267 issue #443's class: a proof whose delivered receipt the full queue refuses is not
// settled; the next round, once the queue has room, proves it.
func TestARefusedDeliveredReceiptLeavesTheProofToTheNextRound(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.facts(true)
	text := pw.deliver(a, true, peerItem(a, 1, startWall, "one"))
	pw.change(a, func(s *state.Store) { s.Owed = fullOwed(a) })
	pw.tr.Record(text, faketranscript.Idle, 0)
	pw.until(func() bool { return strings.Contains(pw.log.String(), "the proof's write failed") })
	if s := pw.store(a); len(s.Awaiting) != 1 || len(s.Pending) != 1 {
		t.Fatalf("a refused receipt settled the proof: %+v", s)
	}
	check(t, pw.e.OwedSent(t.Context(), a, owedTo(a, 10_000)))
	n := pw.e.proof.rounds.Load()
	pw.local(pw.e.pokeProof)
	pw.until(func() bool { return pw.e.proof.rounds.Load() > n })
	if s := pw.store(a); len(s.Awaiting) != 0 || !slices.Contains(owedLines(s), owedLine(1, agent(a, 2), proto.ReceiptDelivered, "")) {
		t.Fatalf("after the next round: %+v", s)
	}
}

// Plan 00267 Task 4.1: an item taken held (the receive rate, the delivery budget, a forge check
// still to run) owes held with its reason.
func TestAHeldItemOwesHeldWithItsReason(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	var batch []team.Item
	for i, why := range []string{proto.ReasonRate, proto.ReasonBudget, proto.ReasonUnresolved, ""} {
		it := peerItem(ops, i+1, int64(i+1), "x")
		if why != "" {
			it.Pending.NotBefore, it.Held = startWall+60_000, why
		}
		batch = append(batch, it)
	}
	w.persist(ops, batch...)
	checkOwed(t, w.store(ops),
		owedLine(1, agent(ops, 2), proto.ReceiptHeld, proto.ReasonRate),
		owedLine(2, agent(ops, 2), proto.ReceiptHeld, proto.ReasonBudget),
		owedLine(3, agent(ops, 2), proto.ReceiptHeld, proto.ReasonUnresolved))
}

// Plan 00267 Task 4.1: a batch the full pending list refuses is fetched again, and its agent
// items owe held(full) meanwhile, written apart from the refused batch.
func TestAFullPendingListOwesHeldFull(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	batch := make([]team.Item, 0, 1792)
	for i := 1; i <= 1792; i++ {
		batch = append(batch, peerItem(ops, i, int64(i), "x"))
	}
	w.persist(ops, batch...)
	_, err := w.e.Persist(t.Context(), team.Batch{Team: ops, NextBatch: "s2", Items: []team.Item{peerItem(ops, 5000, 900, "x"), humanItem(ops, 5001, 901, "")}})
	if !errors.Is(err, state.ErrFull) {
		t.Fatalf("a batch past the agent share = %v, want the refusal", err)
	}
	s := w.store(ops)
	if s.Sync.NextBatch != "s1" || len(s.Pending) != 1792 {
		t.Fatalf("part of the refused batch was kept: next batch %q, %d pending", s.Sync.NextBatch, len(s.Pending))
	}
	checkOwed(t, s, owedLine(5000, agent(ops, 2), proto.ReceiptHeld, proto.ReasonFull))
}

// Plan 00267 Task 4.1: an entry a slot of which does not render is bounced `render` to its
// sender and listed in the drop ledger, both where nothing else goes (no dial) and beside
// entries that do.
func TestAnItemThatDoesNotRenderIsBounced(t *testing.T) {
	for name, alone := range map[string]bool{"alone": true, "beside another": false} {
		t.Run(name, func(t *testing.T) {
			dw := newDeliveryWorld(t, state.DeliverySocket)
			a := tid(t, "alpha")
			dw.live(a)
			bad := peerItem(a, 1, startWall, "x")
			bad.Verdict.Content.Text.To = []string{"not a user id"}
			bad.Verdict.Content.Relation.InReplyTo = "not an event id"
			items := []team.Item{bad}
			if !alone {
				items = append(items, humanItem(a, 2, startWall+1, "fine"))
			}
			dw.facts(false)
			dw.persist(a, items...)
			if !alone {
				dw.next()
			}
			dw.until(func() bool {
				return !slices.ContainsFunc(dw.e.teams[a].store.Pending, func(p state.Pending) bool { return p.ID == evID(1) })
			})
			s := dw.store(a)
			checkOwed(t, s, owedLine(1, agent(a, 2), proto.ReceiptBounced, proto.ReasonRender))
			if len(s.Dropped) != 1 || s.Dropped[0].ID != evID(1) || s.Dropped[0].Reason != proto.ReasonRender {
				t.Fatalf("the drop ledger %+v", s.Dropped)
			}
		})
	}
}

// Plan 00267 Task 4.1: a restored item judged out on its re-fetch is bounced with the verdict's
// reason, and one the server will never serve again `gone`; one merely not newer than its
// room's floor is no loss and owes nothing.
func TestARestoredItemJudgedOutOrGoneIsBounced(t *testing.T) {
	ops := tid(t, "ops")
	judgedOut := peerItem(ops, 1, 100, "x").Verdict
	judgedOut.Outcome, judgedOut.Reason = proto.Drop, proto.ReasonVerb
	for name, tc := range map[string]struct {
		verdict proto.Verdict
		err     error
		want    []string
	}{
		"judged out": {verdict: judgedOut, want: []string{owedLine(1, agent(ops, 2), proto.ReceiptBounced, proto.ReasonVerb)}},
		"gone":       {err: fmt.Errorf("%w: not found", team.ErrItemGone), want: []string{owedLine(1, agent(ops, 2), proto.ReceiptBounced, proto.ReasonGone)}},
		"the floor":  {verdict: peerItem(ops, 1, 40, "x").Verdict},
	} {
		t.Run(name, func(t *testing.T) {
			w := restoredPeerWorld(t, ops, tc.verdict, tc.err)
			_, changed := w.e.PendingOwed(ops)
			w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
			if woken := closed(changed); woken != (len(tc.want) != 0) {
				t.Errorf("the owed sender woken %t, with %d owed", woken, len(tc.want))
			}
			got := w.store(ops)
			if len(got.Pending) != 0 {
				t.Fatalf("pending %+v", got.Pending)
			}
			checkOwed(t, got, tc.want...)
			if len(got.Dropped) != len(tc.want) {
				t.Fatalf("the drop ledger %+v", got.Dropped)
			}
			w.local(func() {
				if n := len(w.e.teams[ops].store.Owed); n != len(tc.want) {
					t.Errorf("memory owes %d", n)
				}
			})
		})
	}
}

// restoredPeerWorld is a world whose store holds one restored PEER item, n 1 sent at 100 below
// a main floor of 50, that its team re-fetches as v, or fails to with err.
func restoredPeerWorld(t *testing.T, ops paths.TeamID, v proto.Verdict, err error) *world {
	t.Helper()
	w := newWorld(t, state.DeliverySocket)
	s := w.newStore(ops)
	s.Sync.FloorMS[proto.PurposeMain] = 50
	p := peerItem(ops, 1, 100, "x").Pending
	p.Restored = true
	s.Pending = []state.Pending{p}
	f := w.add(ops, encodeStore(t, s))
	<-f.running
	if err != nil {
		f.mu.Lock()
		f.refetchErrs = map[string]error{evID(1): err}
		f.mu.Unlock()
		return w
	}
	f.setVerdict(evID(1), v)
	return w
}

// closed is whether ch is closed now.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Plan 00267 Task 4.1: an item a redaction removes is bounced `redacted` to its sender.
func TestARedactedItemIsBounced(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, peerItem(ops, 1, 1, "x"), peerItem(ops, 2, 2, "y"))
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s2", Redacts: []string{evID(1)}})
	s := w.store(ops)
	if len(s.Pending) != 1 || s.Pending[0].ID != evID(2) {
		t.Fatalf("pending %+v", s.Pending)
	}
	checkOwed(t, s, owedLine(1, agent(ops, 2), proto.ReceiptBounced, proto.ReasonRedacted))
}

// Plan 00267 Task 4.1: an own ping's ack, recorded and never shown as an item, owes its sender
// recorded; so does a late answer. An answer delivered as an item owes nothing here (its proof
// owes delivered), nor one to a ping the outbox does not hold.
func TestARecordedAnswerOwesRecorded(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2), agent(ops, 3))
	w.heldWith(ops, w.pingStore(ops, p))
	ack := ans(p, agent(ops, 2), 50, startWall+1000)
	ack.Verb = proto.VerbAck
	late := ans(p, agent(ops, 3), 51, p.DueMS+1)
	done := ans(p, agent(ops, 3), 52, startWall+1000)
	stray := ack
	stray.Ping, stray.ID = evID(9), evID(53)
	shown := peerItem(ops, 52, startWall+1000, "done")
	shown.Pending.From = agent(ops, 3)
	// An on-time `done` is a Deliver verdict, never a recorded one, even with no item beside it.
	onTime := ans(p, agent(ops, 2), 54, startWall+1000)
	w.persistBatch(team.Batch{Team: ops, Items: []team.Item{shown}, Answered: []team.Answer{ack, late, done, stray, onTime}})
	checkOwed(t, w.store(ops),
		owedLine(50, agent(ops, 2), proto.ReceiptRecorded, ""),
		owedLine(51, agent(ops, 3), proto.ReceiptRecorded, ""))
}

// Plan 00267 Task 4.1: a removal bounces every item the team still owes the session, `removed`,
// and posts those bounces with the receipts already owed before the store goes, as the floor
// marker is put; a post that fails is logged.
func TestARemovalBouncesItsItemsBeforeTheStoreGoes(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Owed = []state.Owed{owedTo(ops, 7)}
	w.heldWith(ops, s)
	f := w.fake(ops)
	w.persist(ops, peerItem(ops, 1, 1, "x"), humanItem(ops, 2, 2, "y"))
	w.done(wire.OpRecv, wire.TeamsArgs{})
	f.mu.Lock()
	f.flushOwedErr = errors.New("homeserver down")
	f.mu.Unlock()
	_, err := w.e.Remove(t.Context(), ops)
	check(t, err)
	f.mu.Lock()
	got := slices.Clone(f.flushedOwed)
	f.mu.Unlock()
	want := []state.Owed{
		owedTo(ops, 7),
		{Of: evID(1), To: agent(ops, 2), State: proto.ReceiptBounced, Reason: proto.ReasonRemoved, AtMS: startWall},
		{Of: evID(2), To: alice(ops), State: proto.ReceiptBounced, Reason: proto.ReasonRemoved, AtMS: startWall},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("posted at the removal %+v, want %+v", got, want)
	}
	if !strings.Contains(w.log.String(), "receipts owed were not posted before the removal") {
		t.Fatalf("link.log = %q", w.log.String())
	}
}
