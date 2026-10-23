package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func items(id paths.TeamID, n int) []team.Item {
	out := make([]team.Item, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, humanItem(id, i, int64(i), "hello"))
	}
	return out
}

func more(t *testing.T, out *recorder) int64 {
	t.Helper()
	m, ok := out.recs[len(out.recs)-1].(records.More)
	if !ok {
		t.Fatalf("the last record is %s, not more", out.recs[len(out.recs)-1].Kind())
	}
	return m.Pending
}

func TestARecvHandsOutAtMostEightItems(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, items(ops, 8)...)
	d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" || out.kinds() != strings.Repeat("human,", 7)+"human" || out.flushes != 1 {
		t.Fatalf("recv of 8 = %+v %s", d, out.kinds())
	}
	check(t, w.e.Ack(t.Context(), d.Ack))
	w.persist(ops, humanItem(ops, 20, 30, "x"), humanItem(ops, 21, 31, "x"), humanItem(ops, 22, 32, "x"),
		humanItem(ops, 23, 33, "x"), humanItem(ops, 24, 34, "x"), humanItem(ops, 25, 35, "x"),
		humanItem(ops, 26, 36, "x"), humanItem(ops, 27, 37, "x"), humanItem(ops, 28, 38, "x"))
	d, out = w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" || len(out.recs) != 9 || more(t, out) != 1 {
		t.Fatalf("recv of 9 = %+v %s", d, out.kinds())
	}
	if h, ok := out.recs[0].(records.Human); !ok || h.ID != evID(20) || h.From != "alice" {
		t.Fatalf("the oldest goes first: %+v", out.recs[0])
	}
}

// lineLen is the bytes an item's record takes in the caps, its newline included.
func lineLen(t *testing.T, id paths.TeamID, it team.Item) int {
	t.Helper()
	line, err := records.Encode(itemRecord(id, it.Pending, it.Verdict, namer{server: paths.ServerName(id)}, nil))
	check(t, err)
	return len(line) + 1
}

func TestARecvHandsOutAtMostTwelveKiB(t *testing.T) {
	for name, over := range map[string]int{"exactly 12 KiB": 0, "one byte over": 1} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			ops := tid(t, "ops")
			w.held(ops)
			first := humanItem(ops, 1, 1, "a")
			second := humanItem(ops, 2, 2, "")
			pad := maxBytes - lineLen(t, ops, first) - lineLen(t, ops, second) + over
			second.Verdict.Human.Body = strings.Repeat("b", pad)
			if total := lineLen(t, ops, first) + lineLen(t, ops, second); total != maxBytes+over {
				t.Fatalf("the test's lines are %d bytes", total)
			}
			w.persist(ops, first, second)
			_, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
			want := "human,human"
			if over > 0 {
				want = "human,more"
			}
			if out.kinds() != want {
				t.Fatalf("recv = %s, want %s", out.kinds(), want)
			}
		})
	}
}

func TestTheFirstItemGoesWhateverItsSize(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, humanItem(ops, 1, 1, strings.Repeat("x", maxBytes)))
	if _, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{}); out.kinds() != "human" {
		t.Fatalf("recv = %s", out.kinds())
	}
}

func TestAckDeliversAndReturnRepeats(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, items(ops, 2)...)
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	check(t, w.e.Return(t.Context(), d.Ack))
	s := w.store(ops)
	if len(s.Pending) != 2 || s.Pending[0].Handed != nil || !s.Pending[0].Repeat {
		t.Fatalf("Return = %+v", s.Pending)
	}
	// A late ack, after its items went back, delivers nothing.
	check(t, w.e.Ack(t.Context(), d.Ack))
	if len(w.store(ops).Pending) != 2 {
		t.Fatal("a late ack delivered")
	}
	d = w.done(wire.OpRecv, wire.TeamsArgs{})
	check(t, w.e.Ack(t.Context(), "ffffffff"))
	if len(w.store(ops).Pending) != 2 {
		t.Fatal("an ack nobody was given delivered")
	}
	check(t, w.e.Ack(t.Context(), d.Ack))
	s = w.store(ops)
	if len(s.Pending) != 0 || len(s.Ledger) != 2 || s.Ledger[0].AtMS != startWall || s.Ledger[0].From != alice(ops) {
		t.Fatalf("Ack = pending %+v ledger %+v", s.Pending, s.Ledger)
	}
	if d := w.done(wire.OpRecv, wire.TeamsArgs{}); d.Ack != "" {
		t.Fatalf("a recv of nothing = %+v", d)
	}
}

func TestAClosedConnectionReturnsTheItems(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, items(ops, 9)...)
	for name, out := range map[string]*recorder{
		"record": {err: errors.New("broken pipe")},
		"flush":  {flushErr: errors.New("broken pipe")},
	} {
		raw := []byte(`{"team":null}`)
		d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpRecv, Args: raw}, out)
		if d.Exit != 1 || d.Ack != "" {
			t.Fatalf("%s: recv on a closed connection = %+v", name, d)
		}
		for _, p := range w.store(ops).Pending {
			if p.Handed != nil {
				t.Fatalf("%s: an item stayed handed: %+v", name, p)
			}
		}
	}
}

func TestARestoredItemIsJudgedAgain(t *testing.T) {
	ops := tid(t, "ops")
	ignored := humanItem(ops, 1, 100, "").Verdict
	ignored.Outcome, ignored.Reason = proto.Ignore, "redacted"
	for name, tc := range map[string]struct {
		verdict proto.Verdict
		log     string
	}{
		"its verdict": {ignored, "no longer passes: redacted"},
		"the floor":   {humanItem(ops, 1, 40, "").Verdict, "not newer than its room's floor"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			s := w.newStore(ops)
			s.Sync.FloorMS[proto.PurposeMain] = 50
			s.Pending = []state.Pending{humanItem(ops, 1, 100, "").Pending, humanItem(ops, 2, 200, "").Pending}
			ack := "0badc0de"
			s.Pending[0].Handed = &ack
			f := w.add(ops, encodeStore(t, s))
			f.setVerdict(evID(1), tc.verdict)
			f.setVerdict(evID(2), humanItem(ops, 2, 200, "kept").Verdict)
			_, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
			if out.kinds() != "human" {
				t.Fatalf("recv = %s", out.kinds())
			}
			if p := w.store(ops).Pending; len(p) != 1 || p[0].ID != evID(2) {
				t.Fatalf("pending = %+v", p)
			}
			if !strings.Contains(w.log.String(), tc.log) {
				t.Fatalf("link.log = %q", w.log.String())
			}
		})
	}
}

func TestARefetchThatFailsKeepsTheItemPending(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Pending = []state.Pending{humanItem(ops, 1, 100, "").Pending, humanItem(ops, 2, 200, "").Pending}
	f := w.add(ops, encodeStore(t, s))
	f.mu.Lock()
	f.refetchErr = errors.New("unreachable")
	f.mu.Unlock()
	if d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{}); d.Ack != "" || len(out.recs) != 0 {
		t.Fatalf("recv = %+v %s", d, out.kinds())
	}
	f.mu.Lock()
	tried := len(f.refetched)
	f.mu.Unlock()
	if tried != 1 || len(w.store(ops).Pending) != 2 {
		t.Fatalf("re-fetched %d, pending %d: the team must stop at its first error", tried, len(w.store(ops).Pending))
	}
}

// A1: a restored item the server will never serve again (404, 403) is dropped with its reason,
// and the team's later restored items are still re-fetched and delivered.
func TestARestoredItemThatIsGoneIsDroppedAndHoldsNothing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Pending = []state.Pending{humanItem(ops, 1, 100, "").Pending, humanItem(ops, 2, 200, "").Pending}
	f := w.add(ops, encodeStore(t, s))
	f.mu.Lock()
	f.refetchErrs = map[string]error{evID(1): fmt.Errorf("%w: not found", team.ErrItemGone)}
	f.mu.Unlock()
	f.setVerdict(evID(2), humanItem(ops, 2, 200, "kept").Verdict)
	d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" || out.kinds() != "human" {
		t.Fatalf("recv = %+v %s", d, out.kinds())
	}
	if p := w.store(ops).Pending; len(p) != 1 || p[0].ID != evID(2) {
		t.Fatalf("pending = %+v", p)
	}
	if got := w.store(ops).Sync.FloorMS[proto.PurposeMain]; got != 199 {
		t.Fatalf("floor %d after the drop", got)
	}
	if !strings.Contains(w.log.String(), "restored item "+evID(1)+" dropped: it can no longer be fetched") {
		t.Fatalf("link.log = %q", w.log.String())
	}
}

// A4 (B 2.3.2): an ack that leaves the store owing less raises the floor, to just below the
// oldest item still owed, or to the newest settled item when it owes none, and tells the team
// goroutine, so a quiet room's marker follows.
func TestAnAckRaisesTheFloor(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	first := team.Batch{Team: ops, NextBatch: "s1", Floors: map[string]int64{proto.PurposeMain: 500}, Items: []team.Item{humanItem(ops, 1, 100, "a")}}
	if got := w.persistBatch(first); got != 99 {
		t.Fatalf("floor %d", got)
	}
	ack1 := w.done(wire.OpRecv, wire.TeamsArgs{}).Ack
	w.persist(ops, humanItem(ops, 2, 200, "b"))
	check(t, w.e.Ack(t.Context(), ack1))
	if got := w.store(ops).Sync.FloorMS[proto.PurposeMain]; got != 199 {
		t.Fatalf("floor after the first ack %d", got)
	}
	ack2 := w.done(wire.OpRecv, wire.TeamsArgs{}).Ack
	check(t, w.e.Ack(t.Context(), ack2))
	if got := w.store(ops).Sync.FloorMS[proto.PurposeMain]; got != 200 {
		t.Fatalf("floor after the last ack %d", got)
	}
	check(t, w.e.Return(t.Context(), "ffffffff"))
	f.mu.Lock()
	raised := slices.Clone(f.raised)
	f.mu.Unlock()
	if !slices.Equal(raised, []int64{199, 200}) {
		t.Fatalf("FloorRaised %v", raised)
	}
}

func TestAnOfflineTeamHoldsNoOtherTeamsItems(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	s := w.newStore(ops)
	s.Pending = []state.Pending{humanItem(ops, 1, 1, "").Pending}
	f := w.add(ops, encodeStore(t, s))
	f.mu.Lock()
	f.block = true
	f.mu.Unlock()
	w.held(dev)
	w.persist(dev, humanItem(dev, 2, 2, "from dev"))
	type answer struct {
		d   wire.Done
		out *recorder
	}
	got := make(chan answer, 1)
	go func() {
		out := &recorder{}
		d := w.e.Serve(context.Background(), wire.Request{Op: wire.OpRecv, Args: []byte(`{"team":null}`)}, out)
		got <- answer{d, out}
	}()
	w.awaitDue(refetchWait)
	w.clk.Advance(refetchWait)
	a := <-got
	if a.d.Ack == "" || a.out.kinds() != "human,more" || more(t, a.out) != 1 {
		t.Fatalf("recv = %+v %s", a.d, a.out.kinds())
	}
	if h, ok := a.out.recs[0].(records.Human); !ok || h.Team != dev {
		t.Fatalf("handed %+v", a.out.recs[0])
	}
}

func TestWaitSleepsUntilAnItemIsPersisted(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	got := make(chan wire.Done, 1)
	out := &recorder{}
	go func() {
		got <- w.e.Serve(t.Context(), wire.Request{Op: wire.OpWait, Args: []byte(`{"team":null,"timeout_s":10}`)}, out)
	}()
	w.until(func() bool { return len(w.e.waiters) == 1 })
	w.persist(ops, humanItem(ops, 1, 1, "now"))
	if d := <-got; d.Ack == "" || out.kinds() != "human" {
		t.Fatalf("wait = %+v %s", d, out.kinds())
	}
}

// N1: items returned to pending wake a wait that found everything handed, so an abandoned
// waiter that took them cannot leave a live one asleep past them.
func TestAReturnWakesAWait(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, items(ops, 1)...)
	abandoned := w.done(wire.OpRecv, wire.TeamsArgs{})
	got := make(chan wire.Done, 1)
	out := &recorder{}
	go func() {
		got <- w.e.Serve(t.Context(), wire.Request{Op: wire.OpWait, Args: []byte(`{"team":null,"timeout_s":10}`)}, out)
	}()
	w.until(func() bool { return len(w.e.waiters) == 1 })
	var asleep chan struct{}
	w.local(func() { asleep = w.e.waiters[0] })
	check(t, w.e.Return(t.Context(), abandoned.Ack))
	select {
	case <-asleep:
	default:
		t.Fatal("the wait still sleeps after the return")
	}
	if d := <-got; d.Ack == "" || out.kinds() != "human" {
		t.Fatalf("wait = %+v %s", d, out.kinds())
	}
}

func TestWaitAtItsDeadlineTimesOut(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	ctx, cancel := context.WithCancel(t.Context())
	got := make(chan wire.Done, 1)
	out := &recorder{}
	go func() {
		got <- w.e.Serve(ctx, wire.Request{Op: wire.OpWait, Args: []byte(`{"team":null,"timeout_s":1}`)}, out)
	}()
	w.until(func() bool { return len(w.e.waiters) == 1 })
	cancel()
	if d := <-got; d.Exit != int(exits.ExitTimedOut) || d.Error == nil || d.Error.Code != exits.CodeTimedOut || d.Error.Team != nil || len(out.recs) != 0 {
		t.Fatalf("wait at its deadline = %+v %s", d, out.kinds())
	}
	if d := done(ctx, errors.New("x")); d.Exit != int(exits.ExitTimedOut) {
		t.Fatalf("done at the deadline = %+v", d)
	}
	if d := done(t.Context(), errors.New("x")); d.Exit != 1 {
		t.Fatalf("done before the deadline = %+v", d)
	}
}

func TestWaitRetriesARestoredItemThatCouldNotBeFetched(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Pending = []state.Pending{humanItem(ops, 1, 1, "").Pending}
	f := w.add(ops, encodeStore(t, s))
	f.mu.Lock()
	f.refetchErr = errors.New("unreachable")
	f.mu.Unlock()
	got := make(chan wire.Done, 1)
	out := &recorder{}
	go func() {
		got <- w.e.Serve(t.Context(), wire.Request{Op: wire.OpWait, Args: []byte(`{"team":null,"timeout_s":10}`)}, out)
	}()
	for tried := false; !tried; runtime.Gosched() {
		f.mu.Lock()
		tried = len(f.refetched) == 1
		f.mu.Unlock()
	}
	f.mu.Lock()
	f.refetchErr = nil
	f.verdicts[evID(1)] = humanItem(ops, 1, 1, "back").Verdict
	f.mu.Unlock()
	// The retry's hold may not be armed yet: advance until the wait answers.
	for {
		select {
		case d := <-got:
			if d.Ack == "" || out.kinds() != "human" {
				t.Fatalf("wait = %+v %s", d, out.kinds())
			}
			return
		default:
			w.clk.Advance(waitRetry)
			runtime.Gosched()
		}
	}
}

func TestInboxListsEveryItemOwedAndConsumesNothing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, items(ops, 9)...)
	w.done(wire.OpRecv, wire.TeamsArgs{})
	d, out := w.serve(t.Context(), wire.OpInbox, wire.TeamsArgs{Team: &ops})
	if d != (wire.Done{}) || len(out.recs) != 9 || out.flushes != 1 {
		t.Fatalf("inbox = %+v %s", d, out.kinds())
	}
	if p, ok := out.recs[0].(records.Pending); !ok || p.From != "alice" || p.Item != records.KindHuman {
		t.Fatalf("inbox's first = %+v", out.recs[0])
	}
	if len(w.store(ops).Pending) != 9 {
		t.Fatal("inbox consumed")
	}
	if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpInbox, Args: []byte(`{"team":null}`)}, &recorder{err: errors.New("x")}); d.Exit != 1 {
		t.Fatalf("inbox on a closed connection = %+v", d)
	}
	if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpInbox, Args: []byte(`{"team":null}`)}, &recorder{flushErr: errors.New("x")}); d.Exit != 1 {
		t.Fatalf("inbox with a failed flush = %+v", d)
	}
}
