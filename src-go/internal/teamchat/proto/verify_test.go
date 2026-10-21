package proto

import (
	"errors"
	"testing"
)

func anchors(self string) Anchors {
	return Anchors{Server: testServer, Team: testTeam, MainRoom: mainRoom, Self: self}
}

// roomState is the state a room made from its spec holds: each event sent by the admin, plus
// the create event.
func roomState(t *testing.T, purpose, id string) []StateEvent {
	t.Helper()
	spec := specFor(t, Desired(testServer, testRoster(t), allRooms(), ""), purpose)
	admin := AdminID(testServer)
	state := make([]StateEvent, 0, 1+len(spec.State))
	state = append(state, StateEvent{
		Type: CreateType, Sender: admin, EventID: "$" + id[1:],
		Content: Obj(M("room_version", Str(RoomVersion))),
	})
	for _, e := range spec.State {
		e.Sender = admin
		state = append(state, e)
	}
	return state
}

func trustCheck(err error) string {
	if te, ok := errors.AsType[*TrustError](err); ok {
		return te.Check
	}
	return ""
}

func TestVerifyRoomsMadeFromTheirSpecs(t *testing.T) {
	r, err := VerifyMain(roomState(t, PurposeMain, mainRoom), anchors(workerID))
	if err != nil {
		t.Fatalf("main: %v", err)
	}
	if r.Rev != 42 {
		t.Fatalf("roster rev %d", r.Rev)
	}
	for purpose, id := range map[string]string{PurposeReports: reportsRoom, PurposeQuestions: questionsRoom} {
		if err := VerifySide(purpose, id, roomState(t, purpose, id), r, anchors(workerID)); err != nil {
			t.Errorf("%s: %v", purpose, err)
		}
	}
}

// edit replaces (or with a nil content removes) the state event of a type.
func edit(state []StateEvent, typ string, change func(e *StateEvent) bool) []StateEvent {
	out := make([]StateEvent, 0, len(state))
	for _, e := range state {
		if e.Type == typ && !change(&e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func TestVerifyMainRefuses(t *testing.T) {
	cases := map[string]struct {
		self  string
		typ   string
		check string
		edit  func(e *StateEvent) bool
	}{
		"create sender":      {workerID, CreateType, "create", func(e *StateEvent) bool { e.Sender = workerID; return true }},
		"create version":     {workerID, CreateType, "create", func(e *StateEvent) bool { e.Content = Obj(M("room_version", Str("11"))); return true }},
		"create version int": {workerID, CreateType, "create", func(e *StateEvent) bool { e.Content = Obj(M("room_version", Int(12))); return true }},
		"additional creators": {workerID, CreateType, "create", func(e *StateEvent) bool {
			e.Content = Obj(M("room_version", Str("12")), M("additional_creators", Arr()))
			return true
		}},
		"create event ID":   {workerID, CreateType, "create", func(e *StateEvent) bool { e.EventID = "$" + reportsRoom[1:]; return true }},
		"create missing":    {workerID, CreateType, "create", func(*StateEvent) bool { return false }},
		"join rule":         {workerID, JoinRulesType, "join rule", func(e *StateEvent) bool { e.Content = Obj(M("join_rule", Str("public"))); return true }},
		"join rule sender":  {workerID, JoinRulesType, "join rule", func(e *StateEvent) bool { e.Sender = coordID; return true }},
		"power levels":      {workerID, PowerLevelsType, "power levels I1", func(e *StateEvent) bool { e.Content = Obj(M("users_default", Int(100))); return true }},
		"roster missing":    {workerID, RosterType, "roster", func(*StateEvent) bool { return false }},
		"roster sender":     {workerID, RosterType, "roster", func(e *StateEvent) bool { e.Sender = coordID; return true }},
		"roster key":        {workerID, RosterType, "roster", func(e *StateEvent) bool { e.StateKey = "x"; return true }},
		"roster invalid":    {workerID, RosterType, "roster", func(e *StateEvent) bool { e.Content = Obj(); return true }},
		"member parked":     {parkedID, RosterType, "member", func(*StateEvent) bool { return true }},
		"member not listed": {UserID("x.1+desk.podman", testServer), RosterType, "member", func(*StateEvent) bool { return true }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := VerifyMain(edit(roomState(t, PurposeMain, mainRoom), c.typ, c.edit), anchors(c.self))
			if got := trustCheck(err); got != c.check {
				t.Fatalf("check = %q (%v), want %q", got, err, c.check)
			}
		})
	}
	dup := append(roomState(t, PurposeMain, mainRoom), StateEvent{Type: JoinRulesType, Sender: AdminID(testServer), Content: Obj(M("join_rule", Str("invite")))})
	if _, err := VerifyMain(dup, anchors(workerID)); trustCheck(err) != "join rule" {
		t.Fatalf("a repeated state event gave %v", err)
	}
}

func TestVerifySideRefuses(t *testing.T) {
	r := testRoster(t)
	cases := map[string]struct {
		purpose, id, typ, check string
		edit                    func(e *StateEvent) bool
	}{
		"reports join rule":   {PurposeReports, reportsRoom, JoinRulesType, "join rule", func(e *StateEvent) bool { e.Content = Obj(M("join_rule", Str("invite"))); return true }},
		"reports allow other": {PurposeReports, reportsRoom, JoinRulesType, "join rule", func(e *StateEvent) bool { e.Content = joinRestricted(questionsRoom); return true }},
		"reports allow two": {PurposeReports, reportsRoom, JoinRulesType, "join rule", func(e *StateEvent) bool {
			e.Content = Obj(M("join_rule", Str("restricted")), M("allow", Arr(roomAllow(mainRoom), roomAllow(questionsRoom))))
			return true
		}},
		"questions join rule": {PurposeQuestions, questionsRoom, JoinRulesType, "join rule", func(e *StateEvent) bool { e.Content = joinRestricted(mainRoom); return true }},
		"link missing":        {PurposeReports, reportsRoom, LinkType, "link", func(*StateEvent) bool { return false }},
		"link sender":         {PurposeReports, reportsRoom, LinkType, "link", func(e *StateEvent) bool { e.Sender = coordID; return true }},
		"link purpose": {PurposeReports, reportsRoom, LinkType, "link", func(e *StateEvent) bool {
			e.Content = link(testTeam, PurposeQuestions, mainRoom).Content
			return true
		}},
		"link team": {PurposeQuestions, questionsRoom, LinkType, "link", func(e *StateEvent) bool {
			e.Content = link("other", PurposeQuestions, mainRoom).Content
			return true
		}},
		"link main": {PurposeQuestions, questionsRoom, LinkType, "link", func(e *StateEvent) bool {
			e.Content = link(testTeam, PurposeQuestions, reportsRoom).Content
			return true
		}},
		"link features": {PurposeQuestions, questionsRoom, LinkType, "link", func(e *StateEvent) bool {
			e.Content = Obj(append(link(testTeam, PurposeQuestions, mainRoom).Content.Members(), M("features", Arr()))...)
			return true
		}},
		"power levels": {PurposeReports, reportsRoom, PowerLevelsType, "power levels I4", func(e *StateEvent) bool {
			e.Content = Obj(M("state_default", Int(100)), M("invite", Int(100)), M("kick", Int(100)), M("ban", Int(100)), M("redact", Int(100)))
			return true
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := VerifySide(c.purpose, c.id, edit(roomState(t, c.purpose, c.id), c.typ, c.edit), r, anchors(workerID))
			if got := trustCheck(err); got != c.check {
				t.Fatalf("check = %q (%v), want %q", got, err, c.check)
			}
		})
	}
	if err := VerifySide(PurposeReports, questionsRoom, roomState(t, PurposeReports, questionsRoom), r, anchors(workerID)); trustCheck(err) != "roster" {
		t.Fatalf("a room the roster names under another purpose gave %v", err)
	}
	if err := VerifySide(PurposeMain, reportsRoom, roomState(t, PurposeReports, reportsRoom), r, anchors(workerID)); trustCheck(err) != "roster" {
		t.Fatalf("main as a side purpose gave %v", err)
	}
}

func roomAllow(id string) Value {
	return Obj(M("type", Str("m.room_membership")), M("room_id", Str(id)))
}

func joinRestricted(id string) Value {
	return Obj(M("join_rule", Str("restricted")), M("allow", Arr(roomAllow(id))))
}

func TestPowerLevelInvariants(t *testing.T) {
	base := func(extra ...Member) Value {
		return Obj(append([]Member{
			M("users", Obj(M(aliceID, Int(50)))), M("users_default", Int(0)), M("events_default", Int(100)),
			M("state_default", Int(100)), M("invite", Int(100)), M("kick", Int(100)), M("ban", Int(100)),
			M("redact", Int(100)), M("events", Obj(M(MessageType, Int(0)), M("m.reaction", Int(51)))),
		}, extra...)...)
	}
	if err := CheckPowerLevels(base()); err != nil {
		t.Fatalf("a safe room failed: %v", err)
	}
	cases := map[string]Value{
		"power levels I1": replaceMember(base(), "state_default", Int(50)),
		"power levels I2": replaceMember(base(), "redact", Int(50)),
		"power levels I3": replaceMember(base(), "events", Obj(M("m.reaction", Int(50)))),
		"power levels I4": replaceMember(base(), "events_default", Int(50)),
		"power levels":    replaceMember(base(), "kick", Str("100")),
	}
	for want, content := range cases {
		if got := trustCheck(CheckPowerLevels(content)); got != want {
			t.Errorf("%s: got %q", want, got)
		}
	}
	if trustCheck(CheckPowerLevels(Obj())) != "power levels I2" {
		t.Fatal("Matrix defaults (invite 0) must fail I2")
	}
	if trustCheck(CheckPowerLevels(replaceMember(base(), "users", Obj(M(workerID, Int(100)))))) != "power levels I1" {
		t.Fatal("a user at 100 must break I1")
	}
	if trustCheck(CheckPowerLevels(replaceMember(base(), "users", Obj(M(workerID, Str("1")))))) != "power levels" {
		t.Fatal("a string level accepted")
	}
}

func replaceMember(v Value, key string, val Value) Value {
	members := make([]Member, 0, len(v.Members()))
	for _, m := range v.Members() {
		if m.Key == key {
			m.Val = val
		}
		members = append(members, m)
	}
	return Obj(members...)
}

func TestInviteRule(t *testing.T) {
	admin := AdminID(testServer)
	r := testRoster(t)
	other := "!" + "O" + mainRoom[2:]
	cases := []struct {
		name, room, inviter, creator, self string
		roster                             *Roster
		want                               InviteAction
	}{
		{"main", mainRoom, admin, admin, workerID, nil, InviteAccept},
		{"main other inviter", mainRoom, coordID, admin, workerID, nil, InviteReject},
		{"main other creator", mainRoom, admin, coordID, workerID, nil, InviteReject},
		{"other room before main verified", other, admin, admin, workerID, nil, InvitePending},
		{"other room from a member", other, coordID, admin, workerID, nil, InviteReject},
		{"questions to coordinator", questionsRoom, admin, admin, coordID, &r, InviteAccept},
		{"questions to worker", questionsRoom, admin, admin, workerID, &r, InviteReject},
		{"questions other creator", questionsRoom, admin, coordID, coordID, &r, InviteReject},
		{"reports invite", reportsRoom, admin, admin, workerID, &r, InviteReject},
		{"unnamed room from admin", other, admin, admin, coordID, &r, InvitePending},
		{"unnamed room from member", other, coordID, coordID, coordID, &r, InviteReject},
	}
	for _, c := range cases {
		if got := Invite(c.room, c.inviter, c.creator, anchors(c.self), c.roster); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
