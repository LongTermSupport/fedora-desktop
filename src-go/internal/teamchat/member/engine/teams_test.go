package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func TestAddReturnsEveryHandedItemToPendingMarkedRestored(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	ack := "0badc0de"
	s.Pending = []state.Pending{
		humanItem(ops, 1, 10, "").Pending,
		humanItem(ops, 2, 20, "").Pending,
	}
	s.Pending[1].Handed = &ack
	w.add(ops, encodeStore(t, s))
	got := w.store(ops).Pending
	if got[0].Restored || got[0].Repeat || got[0].Handed != nil {
		t.Fatalf("an item never handed was changed: %+v", got[0])
	}
	if !got[1].Restored || !got[1].Repeat || got[1].Handed != nil {
		t.Fatalf("the handed item was not restored: %+v", got[1])
	}
	// No content is in memory after a restart, so the recv re-fetches both from the team.
	f := w.fake(ops)
	f.setVerdict(evID(2), humanItem(ops, 2, 20, "back").Verdict)
	f.setVerdict(evID(1), humanItem(ops, 1, 10, "first").Verdict)
	d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" || out.kinds() != "human,human" {
		t.Fatalf("recv = %+v, %s", d, out.kinds())
	}
	if h, ok := out.recs[1].(records.Human); !ok || !h.Restored || h.Text != "back" {
		t.Fatalf("the restored item = %+v", out.recs[1])
	}
}

func TestAStoreThatDoesNotDecodeIsTheTeamsFailureOnly(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	f := w.add(ops, []byte(`{"v":1}`))
	w.held(dev)
	if _, err := w.e.Load(ops, agent(ops, 1)); err == nil || !strings.Contains(err.Error(), "does not decode") {
		t.Fatalf("Load of a refused store = %v", err)
	}
	if _, err := w.e.Persist(t.Context(), team.Batch{Team: ops}); err == nil {
		t.Fatal("Persist into a refused store succeeded")
	}
	f.mu.Lock()
	ran := f.ran
	f.mu.Unlock()
	if ran {
		t.Fatal("the team of a refused store was run")
	}
	d, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{Team: &ops})
	if tr, ok := out.recs[0].(records.Team); d.Exit != 0 || !ok || !tr.Down {
		t.Fatalf("status of the refused team = %+v %+v", d, out.recs)
	}
	if !strings.Contains(w.log.String(), "the store is refused") {
		t.Fatalf("link.log = %q", w.log.String())
	}
	refused := false
	for _, s := range w.sentStates() {
		refused = refused || s.Team == ops && s.Store == state.StoreRefused
	}
	if !refused {
		t.Fatalf("the keeper was not told the store is refused: %+v", w.sentStates())
	}
}

func TestAddRefusesAnotherTeamsSeatAndATeamHeldTwice(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	if err := w.e.Add(t.Context(), ops, w.seat(dev, false, nil), nil); err == nil {
		t.Fatal("Add took another team's seat")
	}
	l := w.seat(dev, false, nil)
	check(t, w.e.Add(t.Context(), dev, l, nil))
	f, err := l.Dup()
	check(t, err)
	again, err := lock.AdoptSeat(w.root, f, w.layout.Seat(dev))
	check(t, err)
	if err := w.e.Add(t.Context(), dev, again, nil); err == nil || !strings.Contains(err.Error(), "already held") {
		t.Fatalf("a second Add = %v", err)
	}
	check(t, l.Held())
}

func TestAddWithNoSeatJSONHoldsTheTeamWithNoHandle(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.fake(ops)
	check(t, w.e.Add(t.Context(), ops, w.seat(ops, true, nil), []string{agent(ops, 2)}))
	<-f.running
	if len(f.cfg.RemovedUserIDs) != 1 || f.cfg.Lock == nil {
		t.Fatalf("the team's config = %+v", f.cfg)
	}
	if len(w.sentStates()) != 0 {
		t.Fatal("a team with no handle was reported")
	}
	if !strings.Contains(w.log.String(), "seat.json") {
		t.Fatalf("link.log = %q", w.log.String())
	}
	// The first Load gives the user id, so the store and the report follow.
	s, err := w.e.Load(ops, agent(ops, 1))
	check(t, err)
	if s.Run.ID != w.run || s.Run.Ended != nil || !w.storeExists(ops) {
		t.Fatalf("Load created %+v", s.Run)
	}
	if st := w.sentStates(); len(st) != 1 || st[0].Handle != "repo.1+site.podman" {
		t.Fatalf("the reports = %+v", st)
	}
}

func TestATeamWhoseGoroutineCannotBeMadeIsStoppedAsBundle(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.mu.Lock()
	w.newTeamErr = errors.New("no token")
	w.mu.Unlock()
	check(t, w.e.Add(t.Context(), ops, w.seat(ops, false, encodeStore(t, w.newStore(ops))), nil))
	st := w.status()
	if len(st.Teams) != 1 || st.Teams[0].Failed == nil || *st.Teams[0].Failed != state.FailedBundle {
		t.Fatalf("status.json = %+v", st.Teams)
	}
	if d := w.done(wire.OpVerify, wire.VerifyArgs{ID: evID(1)}); d.Exit != 1 {
		t.Fatalf("verify on a team with no goroutine = %+v", d)
	}
	ends, err := w.e.EndTeams(t.Context())
	check(t, err)
	if len(ends) != 0 {
		t.Fatalf("EndTeams = %+v", ends)
	}
	removed, err := w.e.Remove(t.Context(), ops)
	check(t, err)
	if removed.Notices != nil || removed.UserID != agent(ops, 1) {
		t.Fatalf("Remove = %+v", removed)
	}
}

func TestRemoveFlushesDeletesTheStoreAndClosesTheSeat(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	w.persist(ops, humanItem(ops, 1, 10, "a"), humanItem(ops, 2, 20, "b"))
	d := w.done(wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" {
		t.Fatal("nothing was handed")
	}
	l := f.cfg.Lock
	f.mu.Lock()
	f.flushErr = errors.New("homeserver down")
	f.mu.Unlock()
	removed, err := w.e.Remove(t.Context(), ops)
	check(t, err)
	if removed.UserID != agent(ops, 1) || removed.Notices == nil {
		t.Fatalf("Remove = %+v", removed)
	}
	if f.flushes != 1 || w.storeExists(ops) || l.Held() == nil {
		t.Fatalf("flushes %d, store kept %v, lock held %v", f.flushes, w.storeExists(ops), l.Held())
	}
	logged := w.log.String()
	if !strings.Contains(logged, "dropped 0 pending (2 handed) and 0 awaiting") || !strings.Contains(logged, "floor marker") {
		t.Fatalf("link.log = %q", logged)
	}
	if _, err := w.e.Persist(t.Context(), team.Batch{Team: ops}); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("Persist after the removal = %v", err)
	}
	check(t, w.e.Ack(t.Context(), d.Ack))
	if w.storeExists(ops) {
		t.Fatal("an ack for a removed team recreated its store")
	}
	if _, err := w.e.Remove(t.Context(), ops); !errors.Is(err, errNotHeld) {
		t.Fatalf("a second Remove = %v", err)
	}
	if len(w.status().Teams) != 0 {
		t.Fatal("status.json still shows the team")
	}
}

func TestRemoveWaitsAtMostTwoSecondsForATeamThatIgnoresItsCancel(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.fake(ops)
	f.ignore = make(chan struct{})
	w.add(ops, nil)
	<-f.running
	done := make(chan error, 1)
	go func() {
		_, err := w.e.Remove(t.Context(), ops)
		done <- err
	}()
	w.armed(1)
	w.clk.Advance(stopWait)
	check(t, <-done)
	if !strings.Contains(w.log.String(), "did not stop within 2s") {
		t.Fatalf("link.log = %q", w.log.String())
	}
	close(f.ignore)
}

func TestEndTeamsAndShutdown(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	w.add(dev, nil)
	ends, err := w.e.EndTeams(t.Context())
	check(t, err)
	if len(ends) != 1 || ends[0].Team != ops || ends[0].Notices == nil || ends[0].Run.ID != w.run {
		t.Fatalf("EndTeams = %+v", ends)
	}
	check(t, w.e.Shutdown(t.Context()))
	if d := w.done(wire.OpStatus, wire.TeamsArgs{Team: &ops}); d.Exit != 64 {
		t.Fatalf("status after the shutdown = %+v", d)
	}
}

// SY10, A2: a shutdown puts each running team's floor marker at its clean end, after its team
// goroutine stopped, so no later move is left unput; a failed put is logged and blocks nothing.
func TestShutdownPutsTheFloorMarkers(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	fo, fd := w.held(ops), w.held(dev)
	fd.mu.Lock()
	fd.flushErr = errors.New("homeserver down")
	fd.mu.Unlock()
	check(t, w.e.Shutdown(t.Context()))
	for _, f := range []*fakeTeam{fo, fd} {
		f.mu.Lock()
		flushes, live := f.flushes, f.flushedLive
		f.mu.Unlock()
		if flushes != 1 || live != 0 {
			t.Fatalf("%s: %d flushes, %d while its goroutine ran", f.cfg.Team, flushes, live)
		}
	}
	if !strings.Contains(w.log.String(), "team "+dev.String()+": the floor marker was not put at the shutdown") {
		t.Fatalf("link.log = %q", w.log.String())
	}
}

func TestTheStoreSeamsWriteTheStore(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	check(t, w.e.ResetPosition(t.Context(), ops, 7))
	if s := w.store(ops); s.Sync.NextBatch != "" || s.Sync.RestoreEpoch != 7 {
		t.Fatalf("ResetPosition = %+v", s.Sync)
	}
	restart := state.ReasonRestart
	check(t, w.e.SetEnded(t.Context(), ops, state.RunEnd{AtMS: 5, Notice: state.NoticeNone, Reason: &restart, State: state.StateSkipped}))
	if s := w.store(ops); s.Run.Ended == nil {
		t.Fatalf("SetEnded = %+v", s.Run)
	}
	w.clk.Advance(time.Second)
	check(t, w.e.BeginRun(t.Context(), ops))
	s := w.store(ops)
	if s.Run.Ended != nil || s.Run.StartedMS != startWall+1000 {
		t.Fatalf("BeginRun = %+v", s.Run)
	}
	check(t, w.e.BeginRun(t.Context(), ops))
	if w.store(ops).Run.StartedMS != s.Run.StartedMS {
		t.Fatal("BeginRun of the current run rewrote it")
	}
	if _, err := w.e.Load(dev, agent(dev, 1)); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("Load of a team not held = %v", err)
	}
	w.add(dev, nil)
	if err := w.e.BeginRun(t.Context(), dev); !errors.Is(err, errNoStore) {
		t.Fatalf("BeginRun before the store = %v", err)
	}
}

func TestViewReadsTheLedgerTheOutboxAndOwnPings(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	s.Pending = []state.Pending{humanItem(ops, 1, 10, "").Pending}
	s.Ledger = []state.Ledger{{ID: evID(2), Room: proto.PurposeQuestions, Item: records.KindHuman, From: alice(ops), AtMS: 1}}
	s.Outbox.Pings = []state.Ping{{
		ID: evID(3), Verb: proto.VerbHalt, To: []string{agent(ops, 2)}, SentMS: 5, DueMS: 6,
		Answered: []int{}, Reported: []int{},
	}}
	w.add(ops, encodeStore(t, s))
	r := proto.Roster{Rooms: proto.SideRooms{Questions: qRoom}}
	v, err := w.e.View(ops, []string{evID(1), evID(2), evID(9)}, []string{evID(2), evID(3)}, r)
	check(t, err)
	if !v.Seen(evID(1)) || !v.Seen(evID(2)) || v.Seen(evID(9)) {
		t.Fatalf("SeenIDs = %+v", v.SeenIDs)
	}
	if e, ok := v.Event(evID(2)); !ok || e.RoomID != qRoom || !e.AddressedToSelf {
		t.Fatalf("the ledger's related = %+v", e)
	}
	if e, ok := v.Event(evID(3)); !ok || e.Sender != agent(ops, 1) || e.RoomID != mainRoom || e.Verb != proto.VerbHalt {
		t.Fatalf("the outbox's related = %+v", e)
	}
	if p, ok := v.OwnPing(evID(3)); !ok || p.Verb != proto.VerbHalt || p.OriginTs != 5 {
		t.Fatalf("own ping = %+v", p)
	}
	if roomID(nil, r, "elsewhere") != "" {
		t.Fatal("an unknown purpose has a room")
	}
}

func TestViewOfATeamNotHeldOrWithNoStore(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	dev := tid(t, "dev")
	if _, err := w.e.View(dev, nil, nil, proto.Roster{}); !errors.Is(err, team.ErrRemoved) {
		t.Fatalf("View of a team not held = %v", err)
	}
	w.add(dev, nil)
	v, err := w.e.View(dev, []string{evID(1)}, nil, proto.Roster{})
	check(t, err)
	if len(v.SeenIDs) != 0 {
		t.Fatal("a team with no store saw an id")
	}
}
