package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
	f.notices.setPresence(map[string]string{agent(ops, 1): "online", agent(ops, 2): "unavailable"}, nil)
	w.persistBatch(team.Batch{Team: ops, NextBatch: "s1", Counts: team.Counts{Ignored: 2, Gaps: 1, Dropped: 1, DroppedBy: map[string]int64{proto.ReasonStale: 1}, HumanOverLimit: 3}})
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
		Delivery: state.DeliveryWait, Counts: records.TeamCounts{Ignored: 2, Gaps: 1, Dropped: 1, DroppedBy: map[string]int64{proto.ReasonStale: 1}, HumanOverLimit: 3},
	}
	// Issue #422: status names each drop's reason, not only their number; A 11.1: and the human
	// posts delivered over the flood limit.
	if !reflect.DeepEqual(out.recs[0], records.Record(want)) {
		t.Fatalf("the team record = %+v, want %+v", out.recs[0], want)
	}
	if m, ok := out.recs[1].(records.Member); !ok || !m.Present || m.Presence != presenceOnline || m.Role != "worker" {
		t.Fatalf("self = %+v", out.recs[1])
	}
	if m, ok := out.recs[2].(records.Member); !ok || m.Present || m.Presence != "unavailable" {
		t.Fatalf("the other agent = %+v", out.recs[2])
	}
	for i, st := range []string{roomJoined, roomNotCoordinator, roomAbsent} {
		if r, ok := out.recs[3+i].(records.Room); !ok || r.State != st {
			t.Fatalf("room %d = %+v, want %s", i, out.recs[3+i], st)
		}
	}
	if r, ok := out.recs[6].(records.Team); !ok || r.Team.String() != "dev" || !r.Down || r.Delivery != state.DeliveryNone {
		t.Fatalf("the refused add = %+v", out.recs[6])
	}
}

// R14 (Plan 00265): a member record's presence is the homeserver's for that account (A 6.6),
// asked of every agent in the roster, this member included; present is presence online, so a
// peer whose link syncs shows present. None recorded, or a read that failed, is unknown, never
// a guess; a human in the roster is not asked.
func TestStatusShowsEachMembersPresenceAsTheHomeserverReadsIt(t *testing.T) {
	w := newWorld(t, state.DeliveryWait)
	ops := tid(t, "ops")
	f := w.held(ops)
	f.notices.set(proto.Roster{Members: map[string]proto.RosterMember{
		agent(ops, 1): {Role: "worker"}, agent(ops, 2): {Role: "coordinator"}, agent(ops, 3): {Role: "worker"}, alice(ops): {},
	}})
	f.notices.setPresence(map[string]string{agent(ops, 1): "online", agent(ops, 2): "online"}, nil)
	want := map[string]string{"repo.1+site.podman": presenceOnline, "repo.2+site.podman": presenceOnline, "repo.3+site.podman": presenceUnknown}
	checkPresence(t, w, want)
	f.notices.mu.Lock()
	asked := strings.Join(f.notices.asked, " ")
	f.notices.mu.Unlock()
	if wantAsked := strings.Join([]string{agent(ops, 1), agent(ops, 2), agent(ops, 3)}, " "); asked != wantAsked {
		t.Errorf("asked %q, want %q (no human)", asked, wantAsked)
	}
	f.notices.setPresence(map[string]string{agent(ops, 1): "online", agent(ops, 2): "offline", agent(ops, 3): "unavailable"}, nil)
	want = map[string]string{"repo.1+site.podman": presenceOnline, "repo.2+site.podman": "offline", "repo.3+site.podman": "unavailable"}
	checkPresence(t, w, want)
	f.notices.setPresence(nil, errors.New("the homeserver is away"))
	want = map[string]string{"repo.1+site.podman": presenceUnknown, "repo.2+site.podman": presenceUnknown, "repo.3+site.podman": presenceUnknown}
	checkPresence(t, w, want)
	if logged := w.log.String(); !strings.Contains(logged, "the homeserver is away") {
		t.Errorf("a failed presence read was not logged: %s", logged)
	}
}

// checkPresence serves status and checks each member record's presence, and present with it.
func checkPresence(t *testing.T, w *world, want map[string]string) {
	t.Helper()
	_, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	got := map[string]string{}
	for _, r := range out.recs {
		if m, ok := r.(records.Member); ok {
			got[m.Handle] = m.Presence
			if m.Present != (m.Presence == presenceOnline) {
				t.Errorf("%s: present %v beside presence %q", m.Handle, m.Present, m.Presence)
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("members %v, want %v", got, want)
	}
	for h, p := range want {
		if got[h] != p {
			t.Errorf("%s presence = %q, want %q", h, got[h], p)
		}
	}
}

// questionsRoom is the questions room's record among a status's records.
func questionsRoom(t *testing.T, out *recorder) records.Room {
	t.Helper()
	for _, r := range out.recs {
		if room, ok := r.(records.Room); ok && room.Purpose == proto.PurposeQuestions {
			return room
		}
	}
	t.Fatalf("no questions room record in %s", out.kinds())
	return records.Room{}
}

// Section 6's `room` record for questions, QS2, QS8, QX1: the team's own reading of the room
// (Health.Questions), each state with its check; a worker's is `not-coordinator`, and a roster
// that names no questions room has it `absent`.
func TestStatusShowsTheQuestionsRoomAsTheTeamReadsIt(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops := tid(t, "ops")
	f := w.held(ops)
	r := proto.Roster{
		Members: map[string]proto.RosterMember{agent(ops, 1): {Role: "coordinator"}, alice(ops): {}},
		Rooms:   proto.SideRooms{Questions: qRoom},
	}
	f.notices.set(r)
	cases := []struct {
		health     team.QuestionsHealth
		state      string
		check      string
		withoutQus bool
	}{
		{health: team.QuestionsHealth{State: team.QuestionsReady}, state: roomJoined},
		{health: team.QuestionsHealth{State: team.QuestionsAwaitingInvite}, state: "awaiting-invite"},
		{health: team.QuestionsHealth{State: team.QuestionsLeft}, state: "left"},
		{health: team.QuestionsHealth{State: team.QuestionsBuilding}, state: "building"},
		{health: team.QuestionsHealth{State: team.QuestionsUntrusted, Check: "state"}, state: "untrusted", check: "state"},
		{health: team.QuestionsHealth{State: team.QuestionsUntrusted, Check: team.CheckInvite}, state: "untrusted", check: "invite"},
		{health: team.QuestionsHealth{}, state: "not-coordinator"},
		{health: team.QuestionsHealth{}, state: roomAbsent, withoutQus: true},
	}
	for _, c := range cases {
		if c.withoutQus {
			f.notices.set(proto.Roster{Members: r.Members})
		}
		w.e.SetHealth(ops, team.Health{Role: "coordinator", Questions: c.health})
		_, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
		got := questionsRoom(t, out)
		gotCheck := ""
		if got.Check != nil {
			gotCheck = *got.Check
		}
		if got.State != c.state || gotCheck != c.check {
			t.Errorf("health %+v: the questions room %q check %q, want %q check %q", c.health, got.State, gotCheck, c.state, c.check)
		}
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

// R60: a held team whose homeserver never answered (no roster, no sync, marked down by its
// backoff) is still listed by status, as down, beside a team that answers. Its team record
// carries the team id as its name, its own handle, an empty role (no roster was read to give
// one) and trust unverified; status.json's row is down with a null role.
func TestStatusListsAHeldTeamThatNeverAnswered(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev := tid(t, "ops"), tid(t, "dev")
	f := w.held(ops)
	f.notices.set(proto.Roster{Name: "Operations", Members: map[string]proto.RosterMember{agent(ops, 1): {Role: "worker"}}})
	w.e.SetHealth(ops, team.Health{Role: "worker", LastSyncOKMS: 77, Verified: true})
	w.held(dev)
	w.e.SetHealth(dev, team.Health{Down: true})
	_, out := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	var never *records.Team
	for _, r := range out.recs {
		if tr, ok := r.(records.Team); ok && tr.Team == dev {
			never = &tr
		}
	}
	want := records.Team{
		Team: dev, Name: "dev", Handle: "repo.1+site.podman", Role: "", Trust: trustUnverified,
		Delivery: state.DeliverySocket, Down: true, Counts: records.TeamCounts{DroppedBy: map[string]int64{}},
	}
	if never == nil || !reflect.DeepEqual(*never, want) {
		t.Fatalf("status = %s; the never-answered team = %+v, want %+v", out.kinds(), never, want)
	}
	_, only := w.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{Team: &dev})
	if only.kinds() != "team,runtime" {
		t.Fatalf("status --team of the never-answered team = %s", only.kinds())
	}
	st := w.status()
	if len(st.Teams) != 2 || st.Teams[0].Team != dev || !st.Teams[0].Down || st.Teams[0].Role != nil {
		t.Fatalf("status.json rows = %+v", st.Teams)
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
	if held.Handle == nil || *held.Handle != "repo.1+site.podman" || refused.Handle != nil {
		t.Fatalf("handles %v, %v", held.Handle, refused.Handle)
	}
	if held.Colour != colour.Auto("ops") || refused.Colour != colour.Auto("dev") {
		t.Fatalf("colours %s, %s", held.Colour, refused.Colour)
	}
}

// A seat's duty, as its add gave it, is its row's duty; a seat added with none, and a refused
// team, show null (PROPOSAL-seat-kinds.md).
func TestStatusJSONCarriesTheSeatsDuty(t *testing.T) {
	w := newWorld(t, state.DeliverySocket)
	ops, dev, qa := tid(t, "ops"), tid(t, "dev"), tid(t, "qa")
	f := w.fake(ops)
	check(t, w.e.Add(t.Context(), ops, w.seat(ops, false, encodeStore(t, w.newStore(ops))), nil, duty.Host))
	<-f.running
	w.held(dev)
	check(t, w.e.Refuse(t.Context(), qa, w.run))
	st := w.status()
	if len(st.Teams) != 3 {
		t.Fatalf("status.json = %+v", st)
	}
	byTeam := map[paths.TeamID]state.StatusTeam{}
	for _, row := range st.Teams {
		byTeam[row.Team] = row
	}
	if d := byTeam[ops].Duty; d == nil || *d != duty.Host {
		t.Fatalf("ops duty %v", d)
	}
	if byTeam[dev].Duty != nil || byTeam[qa].Duty != nil {
		t.Fatalf("duties without one given: %v, %v", byTeam[dev].Duty, byTeam[qa].Duty)
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

// R60: the keeper's report of a team carries why the link has it down: the cause the team
// goroutine named, the generic one when it named none, a stop's code, clipped to what
// report.json takes; a team up carries none.
func TestTheKeepersReportCarriesADownTeamsCause(t *testing.T) {
	long := strings.Repeat("x", 2*state.MaxDown)
	for _, c := range []struct {
		health team.Health
		want   *string
	}{
		{team.Health{LastSyncOKMS: 5}, nil},
		{team.Health{Down: true, DownCause: "the homeserver did not answer (dial tcp: i/o timeout)"}, new("the homeserver did not answer (dial tcp: i/o timeout)")},
		{team.Health{Down: true}, new(state.DownNoAnswer)},
		{team.Health{Down: true, DownCause: long}, new(state.ClipDown(long))},
		{team.Health{Stopped: exits.CodeToken}, new("stopped: " + exits.CodeToken)},
		{team.Health{Down: true, DownCause: "x", Stopped: exits.CodeUntrusted}, new("stopped: " + exits.CodeUntrusted)},
	} {
		t.Run(deref(c.want), func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			ops := tid(t, "ops")
			w.held(ops)
			w.e.SetHealth(ops, c.health)
			w.status()
			sent := w.sentStates()
			got := sent[len(sent)-1].Down
			if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
				t.Errorf("health %+v: the report's down = %v, want %v", c.health, deref(got), deref(c.want))
			}
		})
	}
}

// healthInReport is every field of team.Health, each either carried to the keeper's report (a
// pair of healths whose reports differ only through that field) or named with why not. The
// class it guards (R60): a seat reported joined on local proof alone, because a fact of the
// team's reach that status shows never reached report.json, which the host reads. A new field
// of Health fails TestEveryHealthFieldReachesTheReportOrSaysWhyNot until it is placed here.
var healthInReport = map[string]struct {
	base, changed team.Health
	why           string
}{
	"LastSyncOKMS": {base: team.Health{}, changed: team.Health{LastSyncOKMS: 9}},
	"Down":         {base: team.Health{}, changed: team.Health{Down: true}},
	"DownCause":    {base: team.Health{Down: true, DownCause: "a"}, changed: team.Health{Down: true, DownCause: "b"}},
	"Stopped":      {base: team.Health{}, changed: team.Health{Stopped: exits.CodeToken}},
	"Role":         {why: "the seat's role in its team, not whether the team is reached"},
	"Colour":       {why: "a drawing fact of the status line"},
	"Description":  {why: "a drawing fact of the status line"},
	"Coordinator":  {why: "a hook fact, the roster's coordinator"},
	"Verified":     {why: "main is verified before the first sync answer, which last_sync_ok_ms carries"},
	"Questions":    {why: "the questions room, a room record of status; a seat is joined without it"},
}

func TestEveryHealthFieldReachesTheReportOrSaysWhyNot(t *testing.T) {
	fields := map[string]bool{}
	for f := range reflect.TypeFor[team.Health]().Fields() {
		fields[f.Name] = true
		if _, ok := healthInReport[f.Name]; !ok {
			t.Errorf("team.Health.%s is neither carried to report.json nor said why not in healthInReport", f.Name)
		}
	}
	for name, c := range healthInReport {
		if !fields[name] {
			t.Errorf("healthInReport names %s, which team.Health has not", name)
			continue
		}
		if c.why != "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, state.DeliverySocket)
			ops := tid(t, "ops")
			w.held(ops)
			w.e.SetHealth(ops, c.base)
			w.status()
			before := w.sentStates()
			w.e.SetHealth(ops, c.changed)
			w.status()
			after := w.sentStates()
			if len(after) == len(before) || sameState(before[len(before)-1], after[len(after)-1]) {
				t.Errorf("team.Health.%s changed and the keeper's report did not: %+v", name, after[len(after)-1])
			}
		})
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
	e, f, g := d, d, d
	e.Down, f.Down, g.Down = new("x"), new("y"), new("x")
	if sameState(d, e) || sameState(e, f) || !sameState(e, g) {
		t.Fatal("a down cause is not compared by its text")
	}
}
