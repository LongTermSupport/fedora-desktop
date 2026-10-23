package engine

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func (w *world) hookView() HookView {
	w.t.Helper()
	v, err := w.e.HookView(w.t.Context())
	check(w.t, err)
	return v
}

func kindItem(id paths.TeamID, n int, kind string) team.Item {
	it := peerItem(id, n, startWall+int64(n), "x")
	it.Pending.Item = kind
	return it
}

// HK1: in wait mode nothing is pushed, so every pending item is counted by its kind, with the
// team's handle, role, coordinator and description from memory.
func TestTheHookViewCountsWhatIsNotPushed(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	a := tid(t, "alpha")
	w.held(a)
	coord, about := "repo.2+site.podman", "Release work."
	w.e.SetHealth(a, team.Health{Role: proto.RoleWorker, Coordinator: &coord, Description: &about})
	w.persist(a, humanItem(a, 1, startWall, "hi"), kindItem(a, 2, records.KindPeer), kindItem(a, 3, records.KindPing),
		kindItem(a, 4, records.KindNotice))
	v := w.hookView()
	if v.Live || v.Stalled || v.Waiting || v.Delivery != state.DeliveryWait || v.Desired.State != state.DesiredApplied {
		t.Fatalf("view %+v", v)
	}
	if len(v.Teams) != 1 {
		t.Fatalf("teams %+v", v.Teams)
	}
	ht := v.Teams[0]
	want := hooktext.Counts{Total: 4, Humans: 1, Agents: 1, Pings: 1, Notices: 1}
	if ht.Counts != want || len(ht.NotPushed) != 4 || ht.Missing != 0 || ht.Down || ht.Failed != nil {
		t.Fatalf("team %+v", ht)
	}
	if wantTeam := (TeamRole{Team: a, Role: proto.RoleWorker, Handle: "repo.1+site.podman", Coordinator: coord, Description: about}); (TeamRole{
		Team: ht.Team, Role: ht.Role, Handle: ht.Handle, Coordinator: ht.Coordinator, Description: ht.Description,
	}) != wantTeam {
		t.Fatalf("team %+v", ht)
	}
}

// A live socket pushes its pending items, so only those held as missing are counted; a handed
// item is not pending for the hooks.
func TestALiveSocketCountsOnlyTheHeld(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	s := dw.newStore(a)
	s.Pending = []state.Pending{{ID: evID(9), Room: proto.PurposeMain, Item: records.KindHuman, SentMS: startWall, From: alice(a), Held: true, Repeat: true}}
	f := dw.add(a, encodeStore(t, s))
	<-f.running
	f.notices.set(roster(a))
	dw.persist(a, humanItem(a, 1, startWall, "pushed"))
	dw.facts(false)
	dw.next()
	dw.until(func() bool { return len(dw.e.teams[a].store.Awaiting) == 1 })
	v := dw.hookView()
	if !v.Live || len(v.Teams) != 1 {
		t.Fatalf("view %+v", v)
	}
	if ht := v.Teams[0]; !slices.Equal(ht.NotPushed, []string{evID(9)}) || ht.Missing != 1 || ht.Counts.Total != 1 || ht.Counts.Humans != 1 {
		t.Fatalf("team %+v", ht)
	}
}

// 15a major 4: while stalled, delivery is not live for the hooks and every pending item is not
// pushed.
func TestAStallIsNotLiveForTheHooks(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliveryWait)
	a := tid(t, "alpha")
	dw.held(a)
	dw.persist(a, humanItem(a, 1, startWall, "x"))
	dw.local(func() { dw.e.stall.stalled = true })
	v := dw.hookView()
	if v.Live || !v.Stalled || len(v.Teams[0].NotPushed) != 1 {
		t.Fatalf("view %+v", v)
	}
}

// OF1: a down team and a failed one beside a running one, each shown for what it is.
func TestADownAndAFailedTeamInTheView(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	a, b, c := tid(t, "alpha"), tid(t, "bravo"), tid(t, "charlie")
	w.held(a)
	w.held(b)
	w.held(c)
	w.e.SetHealth(b, team.Health{Down: true})
	w.e.SetHealth(c, team.Health{Stopped: exits.CodeToken})
	v := w.hookView()
	if len(v.Teams) != 3 || v.Teams[0].Down || v.Teams[0].Failed != nil || !v.Teams[1].Down || v.Teams[1].Failed != nil ||
		v.Teams[2].Failed == nil || *v.Teams[2].Failed != state.FailedUnauthorized {
		t.Fatalf("teams %+v", v.Teams)
	}
}

// n3c-D23: the desired membership's refusal and a refused add reach the view.
func TestTheRefusalsReachTheView(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	b := tid(t, "bravo")
	code := "schema"
	check(t, w.e.SetDesired(t.Context(), state.ReportDesired{Generation: 3, State: state.DesiredRefused, Code: &code}))
	check(t, w.e.Refuse(t.Context(), b, w.run))
	v := w.hookView()
	if v.Desired.State != state.DesiredRefused || v.Desired.Code == nil || *v.Desired.Code != code || v.Desired.Generation != 3 ||
		!slices.Equal(v.Refused, []paths.TeamID{b}) {
		t.Fatalf("view %+v", v)
	}
}

// HK2's no-waker: Waiting holds exactly while a wait request runs.
func TestWaitingWhileAWaitRuns(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	w.held(tid(t, "alpha"))
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		w.serve(ctx, wire.OpWait, wire.WaitArgs{})
	}()
	for !w.hookView().Waiting {
		if t.Context().Err() != nil {
			t.Fatal("never waiting")
		}
	}
	cancel()
	<-ended
	if w.hookView().Waiting {
		t.Fatal("waiting after the wait ended")
	}
}

type teamsSeen struct {
	mu   sync.Mutex
	seen [][]TeamRole
}

func (s *teamsSeen) record(r []TeamRole) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, r)
}

func (s *teamsSeen) all() [][]TeamRole {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.seen)
}

// DV6: OnTeams is told the held teams whenever one is added, removed or changes its role, and
// not again for a change that leaves them as they were.
func TestOnTeamsIsToldEachChange(t *testing.T) {
	seen := &teamsSeen{}
	w := newWorldWith(t, state.DeliveryWait, func(c *Config) { c.OnTeams = seen.record }, func(*Engine) {})
	a := tid(t, "alpha")
	w.held(a)
	w.e.SetHealth(a, team.Health{Role: proto.RoleWorker})
	w.e.SetHealth(a, team.Health{Role: proto.RoleWorker, LastSyncOKMS: startWall})
	_, err := w.e.Remove(t.Context(), a)
	check(t, err)
	got := seen.all()
	want := [][]TeamRole{
		{},
		{{Team: a, Handle: "repo.1+site.podman"}},
		{{Team: a, Role: proto.RoleWorker, Handle: "repo.1+site.podman"}},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("told %+v", got)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Fatalf("told %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 15a major 3: the hook-stdin count reaches status.json and status's runtime record.
func TestTheUnusableCountsReachStatus(t *testing.T) {
	w := newWorldWith(t, state.DeliveryWait, func(c *Config) { c.Unusable = state.Unusable{HookStdin: 1, Render: 2} }, func(*Engine) {})
	if got := w.status().UnusableHookStdin; got != 1 {
		t.Fatalf("status.json unusable_hook_stdin %d at the start", got)
	}
	check(t, w.e.SetUnusable(t.Context(), state.Unusable{HookStdin: 3, Render: 99}))
	if got := w.status().UnusableHookStdin; got != 3 {
		t.Fatalf("status.json unusable_hook_stdin %d", got)
	}
	_, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	rt, ok := out.recs[len(out.recs)-1].(records.Runtime)
	if !ok || rt.Unusable != (records.Unusable{HookStdin: 3, Render: 2}) {
		t.Fatalf("runtime record %+v", out.recs)
	}
}

// 15c: under -race on the fake clock, a failed slot completes and its count reaches status.
func TestAFailedSlotsCountReachesStatus(t *testing.T) {
	dw := newDeliveryWorld(t, state.DeliverySocket)
	a := tid(t, "alpha")
	dw.live(a)
	bad := humanItem(a, 1, startWall, "edited")
	bad.Verdict.Human.Revises = "not an event id"
	dw.persist(a, bad)
	dw.facts(false)
	dw.next()
	dw.until(func() bool { return dw.e.unusable.Render == 1 })
	_, out := dw.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	if rt, ok := out.recs[len(out.recs)-1].(records.Runtime); !ok || rt.Unusable.Render != 1 {
		t.Fatalf("runtime record %+v", out.recs)
	}
}

func TestATimeoutIsCountedAsOne(t *testing.T) {
	var c hooktext.Counts
	count(&c, records.KindTimeout)
	count(&c, "unknown")
	if c != (hooktext.Counts{Total: 2, Timeouts: 1}) {
		t.Fatalf("counts %+v", c)
	}
}

func TestAnEndedEngineHasNoHookView(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.e.HookView(ctx); err == nil {
		t.Fatal("a view on an ended context")
	}
	if err := w.e.SetUnusable(ctx, state.Unusable{}); err == nil {
		t.Fatal("unusable set on an ended context")
	}
}
