package engine

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// pingTimeoutMS is the seeded pings' own timeout: due_ms less sent_ms.
const pingTimeoutMS = int64(600_000)

var reviewRef = "commit:owner/repo@" + strings.Repeat("a", 40)

// outPing is an own review ping n, sent at sent, to the given user ids, nobody answered yet.
func outPing(n int, sent int64, to ...string) state.Ping {
	return state.Ping{
		ID: evID(n), Verb: "review", Ref: new(reviewRef), To: to, SentMS: sent, DueMS: sent + pingTimeoutMS,
		Answered: []int{}, Reported: []int{},
	}
}

// pingStore is a fresh store of id holding the given outbox pings.
func (w *world) pingStore(id paths.TeamID, pings ...state.Ping) state.Store {
	s := w.newStore(id)
	s.Outbox.Pings = pings
	return s
}

// heldWith is id held over the store s, its team running.
func (w *world) heldWith(id paths.TeamID, s state.Store) {
	w.t.Helper()
	<-w.add(id, encodeStore(w.t, s)).running
}

// advanceTo moves the fake clock until its wall clock reads ms.
func (w *world) advanceTo(ms int64) {
	w.t.Helper()
	d := ms - w.clk.WallMS()
	if d < 0 {
		w.t.Fatalf("advanceTo %d is %d ms in the past", ms, -d)
	}
	w.clk.Advance(time.Duration(d) * time.Millisecond)
}

// restart shuts the engine down and runs a fresh one over the same home, as a restarted link.
func (w *world) restart(delivery string) {
	w.t.Helper()
	first := w.e
	check(w.t, first.Shutdown(w.t.Context()))
	e, err := New(w.config(delivery))
	check(w.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() { ran <- e.Run(ctx) }()
	w.mu.Lock()
	w.fakes = map[paths.TeamID]*fakeTeam{}
	w.mu.Unlock()
	w.e = e
	w.t.Cleanup(func() {
		check(w.t, e.Shutdown(context.Background()))
		cancel()
		check(w.t, <-ran)
		w.e = first
	})
}

// readd holds id again over the store already on disk.
func (w *world) readd(id paths.TeamID) {
	w.t.Helper()
	<-w.add(id, nil).running
}

func timeouts(s state.Store) []state.Pending {
	var out []state.Pending
	for _, p := range s.Pending {
		if p.Item == records.KindTimeout {
			out = append(out, p)
		}
	}
	return out
}

func localOf(id string) string {
	return strings.SplitN(strings.TrimPrefix(id, "@"), ":", 2)[0]
}

func outboxOf(out *recorder) []records.Outbox {
	var got []records.Outbox
	for _, r := range out.recs {
		if o, ok := r.(records.Outbox); ok {
			got = append(got, o)
		}
	}
	return got
}

func (w *world) outbox() []records.Outbox {
	w.t.Helper()
	_, out := w.serve(w.t.Context(), wire.OpStatus, wire.TeamsArgs{})
	return outboxOf(out)
}

func ans(p state.Ping, from string, n int, origin int64) team.Answer {
	return team.Answer{Ping: p.ID, From: from, Verb: "done", ID: evID(n), OriginMS: origin}
}

// SD4-1: one TIMEOUT per silent addressee once the clock passes the deadline and its grace,
// reported in the same write; none at the grace exactly (proto.TimeoutDue: "passes").
func TestSD4_1OneTimeoutPerSilentAddresseePastTheGrace(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2), agent(ops, 3))
	w.heldWith(ops, w.pingStore(ops, p))
	w.persistBatch(team.Batch{Team: ops, Answered: []team.Answer{ans(p, agent(ops, 2), 50, startWall+1000)}})
	if w.clk.Waiters() != 1 {
		t.Fatalf("waiters = %d, want the one TIMEOUT timer", w.clk.Waiters())
	}
	w.advanceTo(p.DueMS + timeoutGraceMS)
	if got := timeouts(w.store(ops)); len(got) != 0 {
		t.Fatalf("a TIMEOUT at the grace exactly: %+v", got)
	}
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	s := w.store(ops)
	want := []state.Pending{{
		ID: proto.TimeoutKey(p.ID, localOf(agent(ops, 3))), Room: proto.PurposeMain, Item: records.KindTimeout,
		SentMS: p.DueMS + timeoutGraceMS, From: agent(ops, 3),
	}}
	if got := timeouts(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("TIMEOUTs = %+v, want %+v", got, want)
	}
	if got := s.Outbox.Pings[0]; !slices.Equal(got.Answered, []int{0}) || !slices.Equal(got.Reported, []int{1}) {
		t.Fatalf("the ping = %+v", got)
	}
	if w.clk.Waiters() != 0 {
		t.Fatalf("waiters = %d with nothing owed", w.clk.Waiters())
	}
}

// SD4-2: an answer stamped at the deadline counts; 1 ms later it does not, the TIMEOUT is
// made, and status shows the answer late.
func TestSD4_2AnAnswerAtTheDeadlineCountsOneLaterDoesNot(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	onTime, late := outPing(1, startWall, agent(ops, 2)), outPing(2, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, onTime, late))
	w.persistBatch(team.Batch{Team: ops, Answered: []team.Answer{
		ans(onTime, agent(ops, 2), 50, onTime.DueMS), ans(late, agent(ops, 2), 51, late.DueMS+1),
	}})
	w.advanceTo(late.DueMS + timeoutGraceMS + 1)
	s := w.store(ops)
	if !slices.Equal(s.Outbox.Pings[0].Answered, []int{0}) || len(s.Outbox.Pings[1].Answered) != 0 {
		t.Fatalf("answered = %+v", s.Outbox.Pings)
	}
	if got := timeouts(s); len(got) != 1 || got[0].ID != proto.TimeoutKey(late.ID, localOf(agent(ops, 2))) {
		t.Fatalf("TIMEOUTs = %+v", got)
	}
	recs := w.outbox()
	if len(recs) != 2 {
		t.Fatalf("outbox records = %+v", recs)
	}
	name := namer{server: paths.ServerName(ops)}.name(agent(ops, 2))
	if a := recs[0].Answers; len(a) != 1 || a[0].Late || *a[0].ID != evID(50) || *a[0].Verb != "done" {
		t.Fatalf("the on-time answer = %+v", a)
	}
	if a := recs[1].Answers; len(a) != 1 || !a[0].Late || *a[0].ID != evID(51) || a[0].From != name {
		t.Fatalf("the late answer = %+v", a)
	}
	if !slices.Equal(recs[1].TimedOut, records.List[string]{name}) {
		t.Fatalf("timed_out = %+v", recs[1].TimedOut)
	}
}

// SD4-3: an on-time answer read after its TIMEOUT was made still counts, and makes no second
// TIMEOUT.
func TestSD4_3AnOnTimeAnswerReadAfterItsTimeout(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	w.persistBatch(team.Batch{Team: ops, Answered: []team.Answer{ans(p, agent(ops, 2), 50, p.DueMS-1)}})
	w.advanceTo(p.DueMS + 2*timeoutGraceMS)
	s := w.store(ops)
	if got := s.Outbox.Pings[0]; !slices.Equal(got.Answered, []int{0}) || !slices.Equal(got.Reported, []int{0}) {
		t.Fatalf("the ping = %+v", got)
	}
	if got := timeouts(s); len(got) != 1 {
		t.Fatalf("TIMEOUTs = %+v", got)
	}
	if a := w.outbox()[0].Answers; len(a) != 1 || a[0].Late {
		t.Fatalf("answers = %+v", a)
	}
}

// SD4-4: an answer from a sender the ping did not address counts for nothing; one to a ping
// the outbox does not hold is ignored with a log line.
func TestSD4_4AnAnswerFromAnUnaddressedSenderCountsForNothing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.persistBatch(team.Batch{Team: ops, Answered: []team.Answer{
		ans(p, agent(ops, 9), 50, startWall+1), ans(outPing(7, startWall), agent(ops, 2), 51, startWall+1),
	}})
	s := w.store(ops)
	if len(s.Outbox.Pings[0].Answered) != 0 {
		t.Fatalf("an unaddressed sender's answer counted: %+v", s.Outbox.Pings[0])
	}
	if a := w.outbox()[0].Answers; len(a) != 0 {
		t.Fatalf("answers = %+v", a)
	}
	if !strings.Contains(w.log.String(), evID(7)+", which is not in the outbox, is ignored") {
		t.Fatalf("link.log = %q", w.log.String())
	}
}

// SD4-5: the ping's echo moves sent_ms and due_ms to the server's time, and the timer follows.
func TestSD4_5AnEchoMovesTheDeadlineAndTheTimerFollows(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.persistBatch(team.Batch{Team: ops, Echoes: []team.Echo{{ID: p.ID, OriginMS: startWall + 5000}, {ID: evID(8), OriginMS: 1}}})
	got := w.store(ops).Outbox.Pings[0]
	if got.SentMS != startWall+5000 || got.DueMS != p.DueMS+5000 {
		t.Fatalf("the echoed ping = %+v", got)
	}
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	if n := len(timeouts(w.store(ops))); n != 0 {
		t.Fatalf("a TIMEOUT on the local send time: %d", n)
	}
	w.advanceTo(got.DueMS + timeoutGraceMS + 1)
	if n := len(timeouts(w.store(ops))); n != 1 {
		t.Fatalf("TIMEOUTs on the echo's time = %d", n)
	}
}

// SD4-9: a settled ping leaves the outbox at the first outbox write at its deadline plus its
// own timeout, not 1 ms before.
func TestSD4_9ASettledPingIsPrunedAtItsDeadlinePlusItsTimeout(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	p.Answered = []int{0}
	w.heldWith(ops, w.pingStore(ops, p))
	w.advanceTo(p.DueMS + pingTimeoutMS - 1)
	w.persistBatch(team.Batch{Team: ops})
	if len(w.store(ops).Outbox.Pings) != 1 || len(w.outbox()) != 1 {
		t.Fatal("the ping was pruned 1 ms early")
	}
	w.advanceTo(p.DueMS + pingTimeoutMS)
	if len(w.outbox()) != 0 {
		t.Fatal("status shows a ping past its window")
	}
	w.persistBatch(team.Batch{Team: ops})
	if got := w.store(ops).Outbox.Pings; len(got) != 0 {
		t.Fatalf("the ping was kept: %+v", got)
	}
}

// SD4-9b: a TIMEOUT still pending past the window keeps its ping, so its record shows the
// ping's verb and ref; once delivered, the ping goes.
func TestSD4_9bAPendingTimeoutKeepsItsPing(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	w.heldWith(ops, w.pingStore(ops, p))
	w.advanceTo(p.DueMS + pingTimeoutMS + 1)
	w.persistBatch(team.Batch{Team: ops})
	if len(w.store(ops).Outbox.Pings) != 1 {
		t.Fatal("the ping went while its TIMEOUT was pending")
	}
	d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if len(out.recs) != 1 {
		t.Fatalf("recv = %+v", out.recs)
	}
	if r, ok := out.recs[0].(records.Timeout); !ok || r.Verb != "review" || r.Ref == nil || *r.Ref != reviewRef {
		t.Fatalf("the TIMEOUT record = %+v", out.recs[0])
	}
	w.persistBatch(team.Batch{Team: ops})
	if len(w.store(ops).Outbox.Pings) != 1 {
		t.Fatal("the ping went while its TIMEOUT was handed")
	}
	check(t, w.e.Ack(t.Context(), d.Ack))
	w.persistBatch(team.Batch{Team: ops})
	if got := w.store(ops).Outbox.Pings; len(got) != 0 {
		t.Fatalf("the ping was kept after its TIMEOUT was delivered: %+v", got)
	}
}

// SD4-10: status's outbox records, oldest first, with short names, the answers and the
// timed-out; a restarted engine prints verb and id null for an answer it did not see.
func TestSD4_10StatusOutboxRecords(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	newer := outPing(1, startWall+10, agent(ops, 2))
	older := outPing(2, startWall, agent(ops, 2), agent(ops, 3), alice(ops))
	w.heldWith(ops, w.pingStore(ops, newer, older))
	w.persistBatch(team.Batch{Team: ops, Answered: []team.Answer{
		ans(older, agent(ops, 2), 50, startWall+1), ans(older, agent(ops, 3), 51, older.DueMS+1),
	}})
	w.advanceTo(older.DueMS + timeoutGraceMS + 1)
	n := namer{server: paths.ServerName(ops)}
	recs := w.outbox()
	if len(recs) != 2 || recs[0].ID != older.ID || recs[1].ID != newer.ID {
		t.Fatalf("outbox records = %+v", recs)
	}
	want := records.Outbox{
		Team: ops, ID: older.ID, Verb: "review", Ref: older.Ref, DueMS: older.DueMS,
		To: records.List[string]{n.name(agent(ops, 2)), n.name(agent(ops, 3)), "alice"},
		Answers: records.List[records.Answer]{
			{From: n.name(agent(ops, 2)), Verb: new("done"), ID: new(evID(50))},
			{From: n.name(agent(ops, 3)), Verb: new("done"), ID: new(evID(51)), Late: true},
		},
		TimedOut: records.List[string]{n.name(agent(ops, 3)), "alice"},
	}
	if !reflect.DeepEqual(recs[0], want) {
		t.Fatalf("the outbox record = %+v\nwant %+v", recs[0], want)
	}
	w.restart(state.DeliverySocket)
	w.readd(ops)
	want.Answers = records.List[records.Answer]{{From: n.name(agent(ops, 2))}}
	if got := w.outbox(); !reflect.DeepEqual(got[0], want) {
		t.Fatalf("after a restart = %+v\nwant %+v", got[0], want)
	}
}

// n3d-D6: a TIMEOUT awaiting its delivery's proof keeps its ping as a pending one does; an
// answer counted twice is one index.
func TestN3dD6AnAwaitingTimeoutKeepsItsPing(t *testing.T) {
	p := outPing(1, startWall, "@a:x", "@b:x")
	p.Reported = []int{1}
	s := state.Store{Outbox: state.Outbox{Pings: []state.Ping{p}}}
	notes, unknown := applyOutbox(&s, team.Batch{Answered: []team.Answer{ans(p, "@a:x", 50, startWall), ans(p, "@a:x", 51, startWall)}})
	if !slices.Equal(s.Outbox.Pings[0].Answered, []int{0}) || len(notes[p.ID]) != 2 || unknown != nil {
		t.Fatalf("answered = %+v, notes %+v", s.Outbox.Pings[0].Answered, notes)
	}
	s.Awaiting = []state.Awaiting{{IDs: []string{evID(9), proto.TimeoutKey(p.ID, "b")}}}
	far := p.DueMS + pingTimeoutMS
	if pruned := pruneOutbox(&s, far); pruned != nil || len(s.Outbox.Pings) != 1 {
		t.Fatalf("an awaiting TIMEOUT's ping was pruned: %v", pruned)
	}
	s.Awaiting = nil
	if pruned := pruneOutbox(&s, far); !slices.Equal(pruned, []string{p.ID}) {
		t.Fatalf("pruned = %v", pruned)
	}
}

// SD4-12: a pending ping's verb comes from its verdict, reaches the ledger at Ack, and the
// lookup reads it from the ledger.
func TestSD4_12APendingPingsVerbReachesTheLedgerAndTheLookup(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	w.persist(ops, team.Item{
		Pending: state.Pending{ID: evID(1), Room: proto.PurposeMain, Item: records.KindPing, SentMS: 1, From: agent(ops, 3)},
		Verdict: proto.Verdict{Outcome: proto.Deliver, Event: proto.Event{ID: evID(1), OriginTs: 1}, Content: proto.Content{Ping: proto.Ping{Verb: "check"}}},
	})
	if v := w.store(ops).Pending[0].Verb; v == nil || *v != "check" {
		t.Fatalf("the pending verb = %v", v)
	}
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	check(t, w.e.Ack(t.Context(), d.Ack))
	if v := w.store(ops).Ledger[0].Verb; v == nil || *v != "check" {
		t.Fatalf("the ledger verb = %v", v)
	}
	v, err := w.e.View(ops, nil, []string{evID(1)}, proto.Roster{})
	check(t, err)
	if v.Events[evID(1)].Verb != "check" {
		t.Fatalf("the lookup = %+v", v.Events[evID(1)])
	}
}

// SY7-4 (engine half): an answer whose batch never persisted leaves the ping unanswered, and
// its TIMEOUT is made.
func TestSY7_4AnAnswerInARefusedBatchLeavesThePingUnanswered(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	p := outPing(1, startWall, agent(ops, 2))
	s := w.pingStore(ops, p)
	for n := range 2048 - 256 {
		s.Pending = append(s.Pending, state.Pending{ID: evID(1000 + n), Room: proto.PurposeMain, Item: records.KindPeer, SentMS: startWall, From: agent(ops, 3)})
	}
	w.heldWith(ops, s)
	peer := team.Item{
		Pending: state.Pending{ID: evID(9), Room: proto.PurposeMain, Item: records.KindPeer, SentMS: startWall, From: agent(ops, 3)},
		Verdict: proto.Verdict{Outcome: proto.Deliver, Event: proto.Event{ID: evID(9), OriginTs: startWall}},
	}
	floor, err := w.e.Persist(t.Context(), team.Batch{Team: ops, NextBatch: "s1", Items: []team.Item{peer}, Answered: []team.Answer{ans(p, agent(ops, 2), 50, startWall+1)}})
	if err == nil || floor != 0 {
		t.Fatalf("a batch into a full pending was persisted (floor %d)", floor)
	}
	w.advanceTo(p.DueMS + timeoutGraceMS + 1)
	got := w.store(ops)
	if len(got.Outbox.Pings[0].Answered) != 0 || len(timeouts(got)) != 1 {
		t.Fatalf("the ping = %+v, TIMEOUTs %d", got.Outbox.Pings[0], len(timeouts(got)))
	}
}

// A removal drops the team's outbox with its store, counted in the log line, and disarms its
// TIMEOUTs.
func TestARemovalDropsTheOutboxAndItsTimer(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.heldWith(ops, w.pingStore(ops, outPing(1, startWall, agent(ops, 2))))
	if _, err := w.e.Remove(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.log.String(), "1 outbox pings and 0 queued sends") {
		t.Fatalf("link.log = %q", w.log.String())
	}
	if w.clk.Waiters() != 0 {
		t.Fatalf("waiters = %d after the removal", w.clk.Waiters())
	}
}
