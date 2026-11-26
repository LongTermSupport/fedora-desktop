package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// unprovenWorld is a world whose store holds one ping, n 1 sent at 100, marked until, and
// whose team re-judges it as v.
func unprovenWorld(t *testing.T, until int64, v proto.Verdict) (*world, paths.TeamID) {
	t.Helper()
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	s := w.newStore(ops)
	p := haltItem(ops, 1, 100).Pending
	p.ForgeUntil = until
	s.Pending = []state.Pending{p}
	f := w.add(ops, encodeStore(t, s))
	f.setVerdict(evID(1), v)
	return w, ops
}

func forgeDrop(ops paths.TeamID, reason string) proto.Verdict {
	v := haltItem(ops, 1, 100).Verdict
	v.Outcome, v.Reason = proto.Drop, reason
	return v
}

// Plan 00267 Task 3.2: a ping whose forge check still fails transiently before its deadline is
// kept, unproven, and held to its next check; nothing is handed and nothing bounced. One never
// checked before (a restored item) is given its deadline then.
func TestAnUnprovenPingStillFailingIsHeldToItsNextCheck(t *testing.T) {
	for name, until := range map[string]int64{"marked": startWall + 1000, "unmarked": 0} {
		t.Run(name, func(t *testing.T) {
			tid := tid(t, "ops")
			w, ops := unprovenWorld(t, until, forgeDrop(tid, proto.ReasonForgeRate))
			if d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{}); d.Ack != "" || len(out.recs) != 0 {
				t.Fatalf("recv = %+v %s", d, out.kinds())
			}
			s := w.store(ops)
			want := until
			if want == 0 {
				want = startWall + proto.ForgeRetryLimitMs
			}
			if len(s.Pending) != 1 || s.Pending[0].ForgeUntil != want || s.Pending[0].NotBefore != startWall+proto.ForgeRetryEveryMs {
				t.Fatalf("pending %+v", s.Pending)
			}
			if len(s.Dropped) != 0 || len(s.Owed) != 0 {
				t.Fatalf("dropped %+v, owed %+v", s.Dropped, s.Owed)
			}
			if !strings.Contains(w.log.String(), "its forge check gave forge-rate; it is checked again until") {
				t.Fatalf("link.log = %q", w.log.String())
			}
		})
	}
}

// Plan 00267 Task 3.2: at its deadline, or on a provenance answer, an unproven ping is dropped
// with the reason, listed in the drop ledger and bounced to its sender.
func TestAnUnprovenPingIsBouncedAtItsDeadline(t *testing.T) {
	for name, tc := range map[string]struct {
		until  int64
		reason string
	}{
		"the deadline": {startWall, proto.ReasonUnresolved},
		"provenance":   {startWall + 1000, proto.ReasonProvenance},
	} {
		t.Run(name, func(t *testing.T) {
			w, ops := unprovenWorld(t, tc.until, forgeDrop(tid(t, "ops"), tc.reason))
			w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
			s := w.store(ops)
			wantDropped := []state.Dropped{{ID: evID(1), Room: proto.PurposeMain, From: agent(ops, 2), SentMS: 100, AtMS: startWall, Reason: tc.reason}}
			wantOwed := []state.Owed{{Of: evID(1), To: agent(ops, 2), State: proto.ReceiptBounced, Reason: tc.reason, AtMS: startWall}}
			if len(s.Pending) != 0 || !slices.Equal(s.Dropped, wantDropped) || !slices.Equal(s.Owed, wantOwed) {
				t.Fatalf("pending %+v, dropped %+v, owed %+v", s.Pending, s.Dropped, s.Owed)
			}
		})
	}
}

// Plan 00267 Task 3.2: an unproven ping the forge now proves is handed.
func TestAnUnprovenPingThatProvesIsHanded(t *testing.T) {
	w, ops := unprovenWorld(t, startWall+1000, haltItem(tid(t, "ops"), 1, 100).Verdict)
	if d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{}); d.Ack == "" || out.kinds() != "ping" {
		t.Fatalf("recv = %+v %s", d, out.kinds())
	}
	if s := w.store(ops); len(s.Dropped) != 0 || len(s.Owed) != 0 {
		t.Fatalf("dropped %+v, owed %+v", s.Dropped, s.Owed)
	}
}

// Plan 00267 Task 3.2: a persisted ping marked unproven keeps no verdict in memory, so it is
// re-fetched, and its forge check made again, before it is handed.
func TestAPersistedUnprovenPingIsCheckedAgainBeforeItIsHanded(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	it := haltItem(ops, 1, 100)
	it.Pending.ForgeUntil = startWall + proto.ForgeRetryLimitMs
	w.persist(ops, it)
	f.setVerdict(evID(1), it.Verdict)
	if d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{}); d.Ack == "" || out.kinds() != "ping" {
		t.Fatalf("recv = %+v %s", d, out.kinds())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.refetched, []string{evID(1)}) {
		t.Fatalf("re-fetched %v", f.refetched)
	}
}

// Plan 00267 Task 3.2: a ping held unproven survives a link restart. The fresh link reads it
// from the store, checks it again, and delivers it once proven, or bounces it at its deadline;
// it is never lost.
func TestAnUnprovenPingSurvivesALinkRestart(t *testing.T) {
	for name, tc := range map[string]struct {
		until  int64
		reason string
	}{
		"proven":       {startWall + 1000, ""},
		"the deadline": {startWall, proto.ReasonUnresolved},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			ops := tid(t, "ops")
			w.held(ops)
			it := haltItem(ops, 1, 100)
			it.Pending.ForgeUntil = tc.until
			w.persist(ops, it)
			w.restart()
			f := w.add(ops, nil)
			<-f.running
			v := it.Verdict
			if tc.reason != "" {
				v = forgeDrop(ops, tc.reason)
			}
			f.setVerdict(evID(1), v)
			d, out := w.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
			s := w.store(ops)
			if tc.reason == "" {
				if d.Ack == "" || out.kinds() != "ping" {
					t.Fatalf("recv after the restart = %+v %s", d, out.kinds())
				}
				return
			}
			if len(s.Pending) != 0 || len(s.Dropped) != 1 || len(s.Owed) != 1 || s.Owed[0].Reason != tc.reason {
				t.Fatalf("after the restart: pending %+v, dropped %+v, owed %+v", s.Pending, s.Dropped, s.Owed)
			}
		})
	}
}
