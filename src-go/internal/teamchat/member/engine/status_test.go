package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

func TestStatusShowsEachTeamItsMembersRoomsAndTheRuntime(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	f := w.held(ops)
	f.notices.set(proto.Roster{
		Name: "Operations",
		Members: map[string]proto.RosterMember{
			agent(ops, 1): {Role: "worker"}, agent(ops, 2): {Role: "coordinator"}, alice(ops): {},
		},
		Rooms: proto.SideRooms{Questions: qRoom},
	})
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s1", Counts: team.Counts{Ignored: 2, Gaps: 1}})
	check(t, w.e.Refuse(t.Context(), dev, w.run))
	d, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	if d != (wire.Done{}) || out.flushes != 1 {
		t.Fatalf("status = %+v", d)
	}
	if out.kinds() != "team,member,member,room,room,room,team,runtime" {
		t.Fatalf("status = %s", out.kinds())
	}
	checkOpsStatus(t, out)
	_, out = w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{Team: &dev})
	if out.kinds() != "team,runtime" {
		t.Fatalf("status --team of a refused add = %s", out.kinds())
	}
}

// checkOpsStatus is TestStatusShowsEachTeamItsMembersRoomsAndTheRuntime's records, in order.
func checkOpsStatus(t *testing.T, out *recorder) {
	t.Helper()
	want := records.Team{
		Team: tid(t, "ops"), Name: "Operations", Handle: "repo.1+site.podman", Role: "worker", Trust: trustTrusted,
		Delivery: state.DeliveryWait, Counts: records.TeamCounts{Ignored: 2, Gaps: 1},
	}
	if out.recs[0] != records.Record(want) {
		t.Fatalf("the team record = %+v, want %+v", out.recs[0], want)
	}
	if m, ok := out.recs[1].(records.Member); !ok || !m.Present || m.Role != "worker" {
		t.Fatalf("self = %+v", out.recs[1])
	}
	if m, ok := out.recs[2].(records.Member); !ok || m.Present {
		t.Fatalf("the other agent = %+v", out.recs[2])
	}
	for i, st := range []string{roomJoined, roomJoined, roomAbsent} {
		if r, ok := out.recs[3+i].(records.Room); !ok || r.State != st {
			t.Fatalf("room %d = %+v, want %s", i, out.recs[3+i], st)
		}
	}
	if r, ok := out.recs[6].(records.Team); !ok || r.Team.String() != "dev" || !r.Down || r.Delivery != state.DeliveryNone {
		t.Fatalf("the refused add = %+v", out.recs[6])
	}
}

func TestStatusOfAnUntrustedOrUnverifiedTeam(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	_, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	if tr, ok := out.recs[0].(records.Team); !ok || tr.Trust != trustUnverified || out.kinds() != "team,runtime" {
		t.Fatalf("unverified = %s %+v", out.kinds(), out.recs[0])
	}
	w.e.SetHealth(ops, team.Health{Stopped: exits.CodeUntrusted, Role: "worker"})
	_, out = w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	if tr, ok := out.recs[0].(records.Team); !ok || tr.Trust != trustUntrusted || !tr.Down || tr.Role != "worker" {
		t.Fatalf("untrusted = %+v", out.recs[0])
	}
}

func TestStatusFailures(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	if d := w.done(wire.OpStatus, wire.TeamsArgs{Team: &dev}); d.Exit != int(exits.ExitUsage) || d.Error.Code != exits.CodeUsage {
		t.Fatalf("status of a team not held = %+v", d)
	}
	w.mu.Lock()
	w.runtimeErr = errors.New("no /proc")
	w.mu.Unlock()
	if _, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{}); out.kinds() != "team" {
		t.Fatalf("status with no runtime = %s", out.kinds())
	}
	if !strings.Contains(w.log.String(), "runtime cost was not read") {
		t.Fatalf("link.log = %q", w.log.String())
	}
	for name, out := range map[string]*recorder{"record": {err: errors.New("x")}, "flush": {flushErr: errors.New("x")}} {
		if d := w.e.Serve(t.Context(), wire.Request{Op: wire.OpStatus, Args: []byte(`{"team":null}`)}, out); d.Exit != 1 {
			t.Fatalf("%s: status on a closed connection = %+v", name, d)
		}
	}
}

func TestStatusJSONAndTheKeepersReport(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	w.e.SetHealth(ops, team.Health{Role: "worker", LastSyncOKMS: 77, Stopped: exits.CodeToken})
	w.e.SetHealth(ops, team.Health{Role: "worker", LastSyncOKMS: 77, Stopped: exits.CodeToken})
	w.e.SetHealth(dev, team.Health{Down: true})
	check(t, w.e.SetDesired(t.Context(), state.ReportDesired{State: state.DesiredRefused}))
	check(t, w.e.Refuse(t.Context(), dev, w.run))
	check(t, w.e.Refuse(t.Context(), ops, w.run))
	st := w.status()
	if st.Desired != state.DesiredRefused || len(st.Teams) != 2 {
		t.Fatalf("status.json = %+v", st)
	}
	row := st.Teams[0]
	if row.Team != ops || row.Role == nil || *row.Role != "worker" || row.Failed == nil || *row.Failed != state.FailedUnauthorized {
		t.Fatalf("the held team's row = %+v", row)
	}
	if st.Teams[1].Team != dev || !st.Teams[1].Down || st.Teams[1].Delivery != state.DeliveryNone {
		t.Fatalf("the refused row = %+v", st.Teams[1])
	}
	checkReports(t, w.sentStates())
}

// B27: a held team's member name is its handle's short name; a refused team's is null, as this
// session holds no seat there. Each colour is the automatic one with no override.
func TestStatusJSONNamesTheMemberAndTheColour(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	check(t, w.e.Refuse(t.Context(), dev, w.run))
	st := w.status()
	if len(st.Teams) != 2 {
		t.Fatalf("status.json = %+v", st)
	}
	held, refused := st.Teams[0], st.Teams[1]
	if held.Member == nil || *held.Member != "repo.1" || refused.Member != nil {
		t.Fatalf("member names %v, %v", held.Member, refused.Member)
	}
	if held.Colour != colour.Auto("ops") || refused.Colour != colour.Auto("dev") {
		t.Fatalf("colours %s, %s", held.Colour, refused.Colour)
	}
}

// OF1: a stopped team is shown beside a running one, each with its member name and colour, so
// the status line draws both.
func TestAStoppedTeamIsDrawnBesideARunningOne(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	w.held(ops)
	w.held(dev)
	w.e.SetHealth(dev, team.Health{Down: true, Stopped: exits.CodeToken})
	st := w.status()
	if len(st.Teams) != 2 {
		t.Fatalf("status.json = %+v", st)
	}
	for _, row := range st.Teams {
		if row.Member == nil || row.Colour != colour.Auto(row.Team.String()) {
			t.Errorf("row %+v", row)
		}
	}
	if stopped := st.Teams[0]; stopped.Team != dev || !stopped.Down || stopped.Failed == nil || st.Teams[1].Down {
		t.Fatalf("rows %+v", st.Teams)
	}
}

// B27: the admin's override, held in Health, is the colour status.json carries.
func TestStatusJSONCarriesTheColourOverride(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	w.held(ops)
	override := "#ffffff"
	w.e.SetHealth(ops, team.Health{Colour: &override})
	if c := w.status().Teams[0].Colour; c != override {
		t.Fatalf("colour %s", c)
	}
}

// checkReports is the keeper's reports: the last with its sync time, and none sent twice.
func checkReports(t *testing.T, reports []wire.State) {
	t.Helper()
	last := reports[len(reports)-1]
	if last.LastSyncOKMS == nil || *last.LastSyncOKMS != 77 || last.Store != state.StoreOK {
		t.Fatalf("the last report = %+v", last)
	}
	synced := 0
	for _, r := range reports {
		if r.LastSyncOKMS != nil {
			synced++
		}
	}
	if synced != 1 {
		t.Fatalf("an unchanged report was sent again: %+v", reports)
	}
}

func TestStatusJSONThatCannotBeWrittenIsLogged(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	w.local(func() {})
	check(t, w.root.RemoveTree(w.layout.Run(w.run).Dir()))
	check(t, w.e.SetDesired(t.Context(), state.ReportDesired{State: state.DesiredRefused}))
	w.clk.Advance(time.Second)
	if !strings.Contains(w.log.String(), "status.json not written") {
		t.Fatalf("link.log = %q", w.log.String())
	}
}

func TestSameStateComparesTheSyncTime(t *testing.T) {
	one, two := int64(1), int64(2)
	a := wire.State{Pending: 1}
	if !sameState(a, a) {
		t.Fatal("a state differs from itself")
	}
	b := a
	b.LastSyncOKMS = &one
	c := a
	c.LastSyncOKMS = &two
	if sameState(a, b) || sameState(b, c) {
		t.Fatal("a sync time change was not seen")
	}
	d := c
	d.LastSyncOKMS = &two
	if !sameState(c, d) {
		t.Fatal("equal sync times differ")
	}
}
