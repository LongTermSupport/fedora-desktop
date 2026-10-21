package proto

import (
	"slices"
	"strings"
	"testing"
)

func allRooms() RoomIDs {
	return RoomIDs{Main: mainRoom, Reports: reportsRoom, Questions: questionsRoom}
}

func specFor(t *testing.T, specs []RoomSpec, purpose string) RoomSpec {
	t.Helper()
	for _, s := range specs {
		if s.Purpose == purpose {
			return s
		}
	}
	t.Fatalf("no spec for %s", purpose)
	return RoomSpec{}
}

func stateOf(t *testing.T, s RoomSpec, typ string) Value {
	t.Helper()
	for _, e := range s.State {
		if e.Type == typ && e.StateKey == "" {
			return e.Content
		}
	}
	t.Fatalf("%s spec has no %s", s.Purpose, typ)
	return Value{}
}

func hasState(s RoomSpec, typ string) bool {
	return slices.ContainsFunc(s.State, func(e StateEvent) bool { return e.Type == typ })
}

func TestDesiredCreationOrder(t *testing.T) {
	r := testRoster(t)
	steps := []struct {
		ids     RoomIDs
		purpose string
	}{
		{RoomIDs{}, PurposeMain},
		{RoomIDs{Main: mainRoom}, PurposeReports},
		{RoomIDs{Main: mainRoom, Reports: reportsRoom}, PurposeQuestions},
	}
	for i, step := range steps {
		specs := Desired(testServer, r, step.ids, "")
		last := specs[len(specs)-1]
		if len(specs) != i+1 || last.Purpose != step.purpose || last.RoomID != "" {
			t.Fatalf("step %d must ask for %s: %+v", i, step.purpose, specs)
		}
		if hasState(specs[0], RosterType) || slices.ContainsFunc(specs, func(s RoomSpec) bool { return s.Invite != nil }) {
			t.Fatalf("step %d writes the roster or invites before all rooms exist", i)
		}
	}
	link := stateOf(t, Desired(testServer, r, RoomIDs{Main: mainRoom}, "")[1], LinkType)
	if string(link.Compact()) != `{"v":1,"team":"myteam","purpose":"reports","main":"`+mainRoom+`"}` {
		t.Fatalf("link = %s", link.Compact())
	}
}

func TestDesiredRosterOnceAllRoomsExist(t *testing.T) {
	r := testRoster(t)
	specs := Desired(testServer, r, allRooms(), "")
	main := specFor(t, specs, PurposeMain)
	roster := stateOf(t, main, RosterType)
	want := r
	want.Rooms = SideRooms{Reports: reportsRoom, Questions: questionsRoom}
	if !roster.Equal(want.Encode()) {
		t.Fatalf("roster in main = %s", roster.Compact())
	}
	if main.Invite == nil {
		t.Fatal("no invites once the roster exists")
	}
}

func TestDesiredMembership(t *testing.T) {
	specs := Desired(testServer, testRoster(t), allRooms(), "")
	main, reports, questions := specFor(t, specs, PurposeMain), specFor(t, specs, PurposeReports), specFor(t, specs, PurposeQuestions)
	wantMain := []string{coordID, lxcWorkerID, workerID, probeID, parkedID, aliceID}
	slices.Sort(wantMain)
	if !slices.Equal(main.Members, wantMain) || !slices.Equal(main.Invite, wantMain) {
		t.Fatalf("main members %v invite %v", main.Members, main.Invite)
	}
	if !slices.Equal(reports.Invite, []string{aliceID}) || !slices.Equal(reports.Members, wantMain) {
		t.Fatalf("reports invite %v members %v", reports.Invite, reports.Members)
	}
	wantQ := []string{coordID, aliceID}
	slices.Sort(wantQ)
	if !slices.Equal(questions.Invite, wantQ) || !slices.Equal(questions.Members, wantQ) {
		t.Fatalf("questions %v / %v", questions.Invite, questions.Members)
	}
	invite, kick := MembershipDiff(questions.Members, []string{aliceID, workerID, AdminID(testServer)}, testServer)
	if !slices.Equal(invite, []string{coordID}) || !slices.Equal(kick, []string{workerID}) {
		t.Fatalf("diff invite %v kick %v", invite, kick)
	}
}

func TestDesiredStateMain(t *testing.T) {
	specs := Desired(testServer, testRoster(t), allRooms(), "")
	main := specFor(t, specs, PurposeMain)
	cases := map[string]string{
		"m.room.name":               `{"name":"My team"}`,
		"m.room.topic":              `{"topic":"Agent team: the team's agents and the humans who direct them. Mention an agent to reach it."}`,
		"m.room.join_rules":         `{"join_rule":"invite"}`,
		"m.room.history_visibility": `{"history_visibility":"shared"}`,
		"m.room.guest_access":       `{"guest_access":"forbidden"}`,
		"m.room.power_levels": `{"users":{"` + aliceID + `":50},"users_default":0,"events_default":100,"state_default":100,` +
			`"invite":100,"kick":100,"ban":100,"redact":100,"notifications":{"room":50},"events":{"m.room.message":0}}`,
	}
	for typ, want := range cases {
		if got := string(stateOf(t, main, typ).Compact()); got != want {
			t.Errorf("main %s = %s\nwant %s", typ, got, want)
		}
	}
	if main.RoomVersion != "12" || hasState(main, "m.room.encryption") || hasState(main, LinkType) {
		t.Fatalf("main create = %+v", main)
	}
	described := specFor(t, Desired(testServer, testRoster(t), allRooms(), "Builds the docs."), PurposeMain)
	if got := string(stateOf(t, described, "m.room.topic").Compact()); got != `{"topic":"Builds the docs."}` {
		t.Fatalf("described topic %s", got)
	}
	bad := specFor(t, Desired(testServer, testRoster(t), allRooms(), " padded "), PurposeMain)
	if !strings.Contains(string(stateOf(t, bad, "m.room.topic").Compact()), "Agent team:") {
		t.Fatal("an invalid description was used")
	}
}

func TestDesiredStateSideRooms(t *testing.T) {
	r := testRoster(t)
	specs := Desired(testServer, r, allRooms(), "")
	reports, questions := specFor(t, specs, PurposeReports), specFor(t, specs, PurposeQuestions)
	cases := map[string]struct {
		spec RoomSpec
		want string
	}{
		"reports name":  {reports, `{"name":"My team · reports"}`},
		"reports join":  {reports, `{"join_rule":"restricted","allow":[{"type":"m.room_membership","room_id":"` + mainRoom + `"}]}`},
		"reports topic": {reports, `{"topic":"Reports from the team's agents, for you to read. To reach an agent, write in the main room; answer questions in the questions room."}`},
		"reports power": {reports, `{"users":{"` + aliceID + `":0},"users_default":10,"events_default":100,"state_default":100,` +
			`"invite":100,"kick":100,"ban":100,"redact":100,"notifications":{"room":100},"events":{"m.room.message":10}}`},
		"questions name":  {questions, `{"name":"My team · questions"}`},
		"questions join":  {questions, `{"join_rule":"invite"}`},
		"questions topic": {questions, `{"topic":"Questions the coordinator myrepo.1+desk.podman could not answer itself. Answer here; the coordinator reads every message in this room."}`},
	}
	for name, c := range cases {
		typ := map[string]string{"name": "m.room.name", "join": "m.room.join_rules", "topic": "m.room.topic", "power": "m.room.power_levels"}[name[strings.LastIndex(name, " ")+1:]]
		if got := string(stateOf(t, c.spec, typ).Compact()); got != c.want {
			t.Errorf("%s = %s\nwant %s", name, got, c.want)
		}
	}
	if !stateOf(t, questions, "m.room.power_levels").Equal(stateOf(t, specFor(t, specs, PurposeMain), "m.room.power_levels")) {
		t.Fatal("open questions power levels differ from main")
	}
	r.Members[coordID] = RosterMember{Role: RoleCoordinator, HumanText: true, Parked: true}
	closed := specFor(t, Desired(testServer, r, allRooms(), ""), PurposeQuestions)
	if got := string(stateOf(t, closed, "m.room.topic").Compact()); got != `{"topic":"This team has no coordinator now, so this room is closed."}` {
		t.Fatalf("closed topic %s", got)
	}
	if ev, ok := stateOf(t, closed, "m.room.power_levels").Get("events"); !ok || string(ev.Compact()) != `{"m.room.message":100}` {
		t.Fatalf("closed questions events %s", ev.Compact())
	}
	if !slices.Equal(closed.Invite, []string{aliceID}) {
		t.Fatalf("closed questions invite %v", closed.Invite)
	}
}

func TestDescription(t *testing.T) {
	for _, ok := range []string{"x", strings.Repeat("é", 120), "Builds the docs."} {
		if !ValidDescription(ok) {
			t.Errorf("ValidDescription(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", " x", "x ", strings.Repeat("a", 121), "a\nb", "a" + string(rune(0x200b)) + "b", "\xff"} {
		if ValidDescription(bad) {
			t.Errorf("ValidDescription(%q) = true", bad)
		}
	}
}
