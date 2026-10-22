package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	server = "myteam.example.org"
	team   = "myteam"
)

var (
	alice  = proto.UserID("alice", server)
	bob    = proto.UserID("bob", server)
	carol  = proto.UserID("carol", server)
	coord  = proto.UserID("myrepo.1+desk.podman", server)
	worker = proto.UserID("myrepo.2+desk.podman", server)
	admin  = proto.AdminID(server)
)

func fixedNow() time.Time { return time.UnixMilli(1_760_000_000_000).UTC() }

// noWait is a matrix clock that never waits.
type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// world is a team's homeserver with its accounts, and the admin client the engine uses.
type world struct {
	srv   *fakehs.Server
	admin *matrix.Admin
	team  Team
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv := fakehs.New(fakehs.Options{ServerName: server, Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	srv.AddUser(proto.AdminLocalpart, "pw", true)
	for _, id := range []string{alice, bob, carol, coord, worker} {
		local, ok := proto.Localpart(id, server)
		if !ok {
			t.Fatalf("%s is not on %s", id, server)
		}
		srv.AddUser(local, "pw", false)
	}
	token, err := srv.MintToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return &world{srv: srv, admin: a, team: Team{Server: server, Team: team, Roster: roster()}}
}

func roster() proto.Roster {
	return proto.Roster{
		Team: team, Name: "My team", Humans: []string{alice, bob},
		Members: map[string]proto.RosterMember{
			coord:  {Role: proto.RoleCoordinator, HumanText: true},
			worker: {Role: proto.RoleWorker, HumanText: true},
		},
		Forge:  proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"},
		Limits: proto.DefaultLimits(),
	}
}

// recorder keeps the recorded room IDs and logs each call; fail names a call that fails.
type recorder struct {
	log  []string
	fail string
	ids  proto.RoomIDs
}

func (r *recorder) Intent(purpose string) error {
	r.log = append(r.log, "intent "+purpose)
	if r.fail == "intent "+purpose {
		return errors.New("disk full")
	}
	return nil
}

func (r *recorder) Record(purpose, roomID string) error {
	r.log = append(r.log, "record "+purpose)
	if r.fail == "record "+purpose {
		return errors.New("disk full")
	}
	switch purpose {
	case proto.PurposeMain:
		r.ids.Main = roomID
	case proto.PurposeReports:
		r.ids.Reports = roomID
	case proto.PurposeQuestions:
		r.ids.Questions = roomID
	}
	return nil
}

func (w *world) converge(t *testing.T, ids proto.RoomIDs) Result {
	t.Helper()
	res, err := Converge(t.Context(), w.admin, &recorder{}, w.team, ids)
	if err != nil {
		t.Fatalf("Converge: %v", err)
	}
	return res
}

// built is a converged new team, its file rev updated as the caller would.
func (w *world) built(t *testing.T) proto.RoomIDs {
	t.Helper()
	res := w.converge(t, proto.RoomIDs{})
	w.team.Roster.Rev = res.RosterRev
	return res.Rooms
}

// kinds are the requests since mark that change something, by kind.
func (w *world) kinds(mark int) []string {
	var out []string
	for _, r := range w.srv.Requests()[mark:] {
		switch {
		case r.Method == http.MethodGet:
		case strings.HasSuffix(r.Path, "/kick"):
			out = append(out, "kick")
		case strings.HasSuffix(r.Path, "/invite"):
			out = append(out, "invite")
		case strings.Contains(r.Path, "/state/"):
			out = append(out, "state")
		case strings.HasSuffix(r.Path, "/createRoom"):
			out = append(out, "create")
		default:
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

func (w *world) mark() int { return len(w.srv.Requests()) }

// newTeamChanges are the CHANGED lines of a new team's first run: the rooms created with A's
// state as their initial state (n2a-D12), so only the roster is written, then the invites.
func newTeamChanges(ids proto.RoomIDs) []string {
	return []string{
		"room main created " + ids.Main, "room reports created " + ids.Reports, "room questions created " + ids.Questions,
		"room main state teamchat.roster",
		"room main invited " + alice, "room main invited " + bob, "room main invited " + coord, "room main invited " + worker,
		"room reports invited " + alice, "room reports invited " + bob,
		"room questions invited " + alice, "room questions invited " + bob, "room questions invited " + coord,
	}
}

func TestConvergeNewTeam(t *testing.T) {
	w := newWorld(t)
	rec := &recorder{}
	res, err := Converge(t.Context(), w.admin, rec, w.team, proto.RoomIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if want := newTeamChanges(res.Rooms); !slices.Equal(res.Changed, want) {
		t.Fatalf("changed:\n%s\nwant:\n%s", strings.Join(res.Changed, "\n"), strings.Join(want, "\n"))
	}
	if res.RosterRev != 1 || rec.ids != res.Rooms {
		t.Errorf("roster rev %d (want 1), recorded %+v", res.RosterRev, rec.ids)
	}
	wantLog := []string{"intent main", "record main", "intent reports", "record reports", "intent questions", "record questions"}
	if !slices.Equal(rec.log, wantLog) {
		t.Errorf("recorder log %v", rec.log)
	}
	wantKinds := append([]string{"create", "create", "create", "state"}, slices.Repeat([]string{"invite"}, 9)...)
	if got := w.kinds(0); !slices.Equal(got, wantKinds) {
		t.Errorf("write order %v", got)
	}
}

func TestConvergeIsIdempotent(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	mark := w.mark()
	res := w.converge(t, ids)
	if len(res.Changed) != 0 || res.RosterRev != 0 || res.Rooms != ids {
		t.Fatalf("second run = %+v", res)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("second run wrote %v", writes)
	}
}

// TestConvergedRoomsAreTrusted is the round trip RE10: proto's own trust checks accept what
// the engine made.
func TestConvergedRoomsAreTrusted(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	anchors := proto.Anchors{Server: server, Team: team, MainRoom: ids.Main, Self: worker}
	got, err := proto.VerifyMain(w.state(t, ids.Main), anchors)
	if err != nil {
		t.Fatalf("VerifyMain: %v", err)
	}
	want := roster()
	want.Rev, want.Rooms = 1, proto.SideRooms{Reports: ids.Reports, Questions: ids.Questions}
	if !got.Encode().Equal(want.Encode()) {
		t.Fatalf("roster read back %s, want %s", got.Encode().Compact(), want.Encode().Compact())
	}
	for purpose, id := range map[string]string{proto.PurposeReports: ids.Reports, proto.PurposeQuestions: ids.Questions} {
		if err := proto.VerifySide(purpose, id, w.state(t, id), got, anchors); err != nil {
			t.Errorf("VerifySide(%s): %v", purpose, err)
		}
	}
}

func (w *world) state(t *testing.T, roomID string) []proto.StateEvent {
	t.Helper()
	raw, err := w.admin.RoomState(t.Context(), roomID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := parseState(raw)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestConvergeAdoptsAfterACrash(t *testing.T) {
	for _, purpose := range []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions} {
		w := newWorld(t)
		crashed := &recorder{fail: "record " + purpose}
		_, err := Converge(t.Context(), w.admin, crashed, w.team, proto.RoomIDs{})
		if e, ok := errors.AsType[*Error](err); !ok || e.Purpose != purpose || e.Step != StepRecord || e.Drift {
			t.Fatalf("%s: crash = %v", purpose, err)
		}
		lost := slices.DeleteFunc(w.adminCreated(t), func(id string) bool {
			return id == crashed.ids.Main || id == crashed.ids.Reports || id == crashed.ids.Questions
		})
		if len(lost) != 1 {
			t.Fatalf("%s: unrecorded rooms %v", purpose, lost)
		}
		res, err := Converge(t.Context(), w.admin, &recorder{}, w.team, crashed.ids)
		if err != nil {
			t.Fatalf("%s: after the crash: %v", purpose, err)
		}
		if !slices.Contains(res.Changed, "room "+purpose+" adopted "+lost[0]) {
			t.Errorf("%s: no adoption of %s in %v", purpose, lost[0], res.Changed)
		}
		if after := w.adminCreated(t); len(after) != 3 {
			t.Errorf("%s: %d rooms after adoption, want 3", purpose, len(after))
		}
	}
}

// adminCreated are the rooms the admin has joined and created: the server's own admin room,
// which every admin is in on Tuwunel (fakehs's AdminRoom quirk), is not one.
func (w *world) adminCreated(t *testing.T) []string {
	t.Helper()
	joined, err := w.admin.JoinedRooms(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(joined, func(id string) bool {
		return !slices.ContainsFunc(w.state(t, id), func(ev proto.StateEvent) bool {
			return ev.Type == proto.CreateType && ev.Sender == admin
		})
	})
}

func (w *world) createLinked(t *testing.T, teamID, purpose, mainID string) string {
	t.Helper()
	link := proto.Obj(proto.M("v", proto.Int(1)), proto.M("team", proto.Str(teamID)), proto.M("purpose", proto.Str(purpose)),
		proto.M("main", proto.Str(mainID)))
	id, err := w.admin.CreateRoom(t.Context(), matrix.CreateRoomRequest{
		Preset: "private_chat", RoomVersion: proto.RoomVersion,
		InitialState: []matrix.StateEvent{{Type: proto.LinkType, Content: link.Compact()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestConvergeRefusesTwoCandidates(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	extra := w.createLinked(t, team, proto.PurposeReports, ids.Main)
	mark := w.mark()
	_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, proto.RoomIDs{Main: ids.Main, Questions: ids.Questions})
	e, ok := errors.AsType[*Error](err)
	if !ok || !e.Drift || e.Step != StepAdopt || e.Purpose != proto.PurposeReports ||
		!strings.Contains(err.Error(), ids.Reports) || !strings.Contains(err.Error(), extra) {
		t.Fatalf("two candidates = %v", err)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("refusal wrote %v", writes)
	}
}

func TestConvergeRefusesALinkToAnotherMain(t *testing.T) {
	w := newWorld(t)
	other := "!" + strings.Repeat("X", 43)
	w.createLinked(t, team, proto.PurposeReports, other)
	_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, proto.RoomIDs{})
	if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepAdopt || !strings.Contains(err.Error(), other) {
		t.Fatalf("link to another main = %v", err)
	}
}

func TestConvergeIgnoresOtherTeamsAndPurposes(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	w.createLinked(t, team, proto.PurposeQuestions, ids.Main)
	res := w.converge(t, proto.RoomIDs{Main: ids.Main, Questions: ids.Questions})
	if !slices.Equal(res.Changed, []string{"room reports adopted " + ids.Reports}) {
		t.Fatalf("changed %v", res.Changed)
	}
}

func TestConvergeRefusesTwoMainCandidates(t *testing.T) {
	w := newWorld(t)
	for range 2 {
		if _, err := w.admin.CreateRoom(t.Context(), matrix.CreateRoomRequest{Preset: "private_chat"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, proto.RoomIDs{})
	if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepAdopt || e.Purpose != proto.PurposeMain {
		t.Fatalf("two main candidates = %v", err)
	}
}

// TestConvergeIgnoresTheServersAdminRoom is the server's own admin room, which Tuwunel creates
// as its server user and joins a server admin to (seen against the real server; fakehs's
// AdminRoom quirk): it has no link, but it is not the team's main room (n2a-D7).
func TestConvergeIgnoresTheServersAdminRoom(t *testing.T) {
	w := newWorld(t)
	joined, err := w.admin.JoinedRooms(t.Context())
	if err != nil || len(joined) != 1 {
		t.Fatalf("a new server's admin is in %v (%v), want the admin room alone", joined, err)
	}
	res := w.converge(t, proto.RoomIDs{})
	if res.Rooms.Main == joined[0] || !slices.Contains(res.Changed, "room main created "+res.Rooms.Main) {
		t.Fatalf("main %s from the admin room %s: %v", res.Rooms.Main, joined[0], res.Changed)
	}
}

// TestConvergeAdoptsOnlyTheTeamsSideRoom is RE2 for a side room: beside the real reports room,
// a room with a link naming another team, a link another user sent, or a correct link in a room
// another user created is not a candidate, so the one real room is adopted rather than two
// refused.
func TestConvergeAdoptsOnlyTheTeamsSideRoom(t *testing.T) {
	cases := map[string]struct {
		team string
		edit func([]map[string]any) []map[string]any
	}{
		"another team":         {team: "otherteam", edit: setOn(proto.LinkType, func(map[string]any) {})},
		"link from another":    {team: team, edit: setOn(proto.LinkType, func(ev map[string]any) { ev["sender"] = alice })},
		"created by another":   {team: team, edit: setOn(proto.CreateType, func(ev map[string]any) { ev["sender"] = alice })},
		"the team's own (ctl)": {team: team},
	}
	for name, c := range cases {
		w := newWorld(t)
		ids := w.built(t)
		extra := w.createLinked(t, c.team, proto.PurposeReports, ids.Main)
		var client Client = w.admin
		if c.edit != nil {
			client = editing{Client: w.admin, roomID: extra, edit: c.edit}
		}
		res, err := Converge(t.Context(), client, &recorder{}, w.team, proto.RoomIDs{Main: ids.Main, Questions: ids.Questions})
		if c.edit == nil {
			if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepAdopt {
				t.Errorf("%s: two real candidates = %v", name, err)
			}
			continue
		}
		if err != nil || !slices.Equal(res.Changed, []string{"room reports adopted " + ids.Reports}) {
			t.Errorf("%s: %v, %v", name, res.Changed, err)
		}
	}
}

func TestConvergeRefusesOneRoomForTwoPurposes(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	mark := w.mark()
	_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, proto.RoomIDs{Main: ids.Main, Reports: ids.Main, Questions: ids.Questions})
	if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepRead || e.Purpose != proto.PurposeReports ||
		!strings.Contains(err.Error(), ids.Main) {
		t.Fatalf("one room for main and reports = %v", err)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("refusal wrote %v", writes)
	}
}

// TestConvergeReportsWhatChangedBeforeAFailure is RE9's partial result: the CHANGED lines and
// the roster rev the server accepted before the failed step.
func TestConvergeReportsWhatChangedBeforeAFailure(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	w.team.Roster.Humans = []string{alice, carol}
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/invite", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN"}`)})
	res, err := Converge(t.Context(), w.admin, &recorder{}, w.team, ids)
	if e, ok := errors.AsType[*Error](err); !ok || e.Step != StepInvite {
		t.Fatalf("invite failure = %v", err)
	}
	want := []string{
		"room main kicked " + bob, "room reports kicked " + bob, "room questions kicked " + bob,
		"room main state m.room.power_levels", "room main state teamchat.roster",
		"room reports state m.room.power_levels", "room questions state m.room.power_levels",
	}
	if !slices.Equal(res.Changed, want) || res.RosterRev != 2 || res.Rooms != ids {
		t.Fatalf("partial result %+v, want changed:\n%s", res, strings.Join(want, "\n"))
	}
}

func TestSlotRefusesAnUnknownPurpose(t *testing.T) {
	if _, err := (&engine{}).slot("lobby"); err == nil {
		t.Fatal("an unknown purpose was set")
	}
}

// TestConvergeOrder is C 3.14's "kicks before state before invites" and "a removed human gone
// from every room before power levels change".
func TestConvergeOrder(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	w.team.Roster.Humans = []string{alice, carol}
	mark := w.mark()
	res := w.converge(t, ids)
	want := []string{"kick", "kick", "kick", "state", "state", "state", "state", "invite", "invite", "invite"}
	if got := w.kinds(mark); !slices.Equal(got, want) {
		t.Fatalf("write order %v, want %v", got, want)
	}
	wantChanged := []string{
		"room main kicked " + bob, "room reports kicked " + bob, "room questions kicked " + bob,
		"room main state m.room.power_levels", "room main state teamchat.roster",
		"room reports state m.room.power_levels", "room questions state m.room.power_levels",
		"room main invited " + carol, "room reports invited " + carol, "room questions invited " + carol,
	}
	if !slices.Equal(res.Changed, wantChanged) {
		t.Fatalf("changed:\n%s", strings.Join(res.Changed, "\n"))
	}
	if res.RosterRev != 2 {
		t.Errorf("roster rev %d, want 2", res.RosterRev)
	}
}

func TestConvergeReinvitesWhoLeft(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	token, err := w.srv.MintToken(alice)
	if err != nil {
		t.Fatal(err)
	}
	m, err := matrix.NewMember(matrix.Config{BaseURL: w.srv.URL(), PlainHTTPHosts: []string{"127.0.0.1"}, Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	if _, err := m.Join(t.Context(), ids.Questions); err != nil {
		t.Fatal(err)
	}
	if err := m.Leave(t.Context(), ids.Questions); err != nil {
		t.Fatal(err)
	}
	res := w.converge(t, ids)
	if !slices.Equal(res.Changed, []string{"room questions invited " + alice}) {
		t.Fatalf("changed %v", res.Changed)
	}
}

func TestConvergeRewritesDifferingState(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	w.team.Description = "Builds the widgets."
	res := w.converge(t, ids)
	if !slices.Equal(res.Changed, []string{"room main state m.room.topic"}) || res.RosterRev != 0 {
		t.Fatalf("changed %v rev %d", res.Changed, res.RosterRev)
	}
	topic, err := w.admin.GetState(t.Context(), ids.Main, "m.room.topic", "")
	if err != nil || string(topic) != `{"topic":"Builds the widgets."}` {
		t.Fatalf("topic %s, %v", topic, err)
	}
}

func TestConvergeRosterRev(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	// The file's rev ahead of the event's: compared without rev, nothing to write.
	w.team.Roster.Rev = 5
	if res := w.converge(t, ids); len(res.Changed) != 0 || res.RosterRev != 0 {
		t.Fatalf("rev-only difference wrote %+v", res)
	}
	w.team.Roster.Members[worker] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true, Parked: true}
	res := w.converge(t, ids)
	if res.RosterRev != 6 || !slices.Equal(res.Changed, []string{"room main state teamchat.roster"}) {
		t.Fatalf("file ahead: %+v, want rev 6", res)
	}
	// The event's rev ahead of the file's.
	w.team.Roster.Rev = 2
	w.team.Roster.Members[worker] = proto.RosterMember{Role: proto.RoleWorker, HumanText: true}
	if res := w.converge(t, ids); res.RosterRev != 7 {
		t.Fatalf("event ahead: %+v, want rev 7", res)
	}
}

func TestConvergeRefusesAnInvalidRosterBeforeAnyWrite(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	w.team.Roster.Name = ""
	w.team.Roster.Humans = []string{alice, carol}
	mark := w.mark()
	_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, ids)
	if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepState || e.Purpose != proto.PurposeMain ||
		!strings.Contains(err.Error(), "roster invalid: name") {
		t.Fatalf("invalid roster = %v", err)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("an invalid roster still wrote %v", writes)
	}
}

// editing is the admin client with one room's state changed as read.
type editing struct {
	Client

	roomID string
	edit   func(events []map[string]any) []map[string]any
}

func (e editing) RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error) {
	raw, err := e.Client.RoomState(ctx, roomID)
	if err != nil || roomID != e.roomID {
		return raw, err
	}
	events := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		var ev map[string]any
		if err := json.Unmarshal(r, &ev); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	out := make([]json.RawMessage, 0, len(events))
	for _, ev := range e.edit(events) {
		b, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func setOn(typ string, change func(ev map[string]any)) func([]map[string]any) []map[string]any {
	return func(events []map[string]any) []map[string]any {
		for _, ev := range events {
			if ev["type"] == typ {
				change(ev)
			}
		}
		return events
	}
}

func TestConvergeRefusesDriftItMustNotRepair(t *testing.T) {
	cases := map[string]func([]map[string]any) []map[string]any{
		"create by another": setOn(proto.CreateType, func(ev map[string]any) { ev["sender"] = alice }),
		"room version": setOn(proto.CreateType, func(ev map[string]any) {
			ev["content"] = map[string]any{"room_version": "11"}
		}),
		"another creator": setOn(proto.CreateType, func(ev map[string]any) {
			ev["content"] = map[string]any{"room_version": "12", "additional_creators": []any{alice}}
		}),
		"no create": func(events []map[string]any) []map[string]any {
			return slices.DeleteFunc(events, func(ev map[string]any) bool { return ev["type"] == proto.CreateType })
		},
		"encrypted": func(events []map[string]any) []map[string]any {
			return append(events, map[string]any{
				"type": "m.room.encryption", "state_key": "", "sender": admin, "event_id": "$e",
				"content": map[string]any{"algorithm": "m.megolm.v1.aes-sha2"},
			})
		},
	}
	for name, edit := range cases {
		w := newWorld(t)
		ids := w.built(t)
		mark := w.mark()
		_, err := Converge(t.Context(), editing{Client: w.admin, roomID: ids.Reports, edit: edit}, &recorder{}, w.team, ids)
		if e, ok := errors.AsType[*Error](err); !ok || !e.Drift || e.Step != StepRead || e.Purpose != proto.PurposeReports {
			t.Errorf("%s: %v", name, err)
		}
		if writes := w.kinds(mark); len(writes) != 0 {
			t.Errorf("%s: drift still wrote %v", name, writes)
		}
	}
}

func TestConvergeRewritesStateFromAnotherSender(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	foreign := setOn("m.room.topic", func(ev map[string]any) { ev["sender"] = alice })
	res, err := Converge(t.Context(), editing{Client: w.admin, roomID: ids.Main, edit: foreign}, &recorder{}, w.team, ids)
	if err != nil || !slices.Equal(res.Changed, []string{"room main state m.room.topic"}) {
		t.Fatalf("foreign topic: %v %v", res.Changed, err)
	}
}

func TestConvergeRefusesMalformedState(t *testing.T) {
	cases := map[string]func([]map[string]any) []map[string]any{
		"no type":        setOn("m.room.topic", func(ev map[string]any) { delete(ev, "type") }),
		"content string": setOn("m.room.topic", func(ev map[string]any) { ev["content"] = "x" }),
		"key number":     setOn("m.room.topic", func(ev map[string]any) { ev["state_key"] = 1 }),
		"no key":         setOn("m.room.topic", func(ev map[string]any) { delete(ev, "state_key") }),
		"sender missing": setOn("m.room.topic", func(ev map[string]any) { delete(ev, "sender") }),
		"twice": func(events []map[string]any) []map[string]any {
			return append(events, events[len(events)-1])
		},
	}
	for name, edit := range cases {
		w := newWorld(t)
		ids := w.built(t)
		_, err := Converge(t.Context(), editing{Client: w.admin, roomID: ids.Main, edit: edit}, &recorder{}, w.team, ids)
		if e, ok := errors.AsType[*Error](err); !ok || e.Drift || e.Step != StepRead {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestConvergeNamesTheFailedStep(t *testing.T) {
	cases := []struct {
		name, method, path, step string
		fresh                    bool
	}{
		{"joined rooms", http.MethodGet, "/joined_rooms", StepFind, true},
		{"create", http.MethodPost, "/createRoom", StepCreate, true},
		{"read", http.MethodGet, "/state", StepRead, false},
		{"kick", http.MethodPost, "/kick", StepKick, false},
		{"state", http.MethodPut, "/state/", StepState, false},
		{"invite", http.MethodPost, "/invite", StepInvite, false},
	}
	for _, c := range cases {
		w := newWorld(t)
		ids := proto.RoomIDs{}
		if !c.fresh {
			ids = w.built(t)
			w.team.Roster.Humans = []string{alice, carol}
		}
		w.srv.Inject(fakehs.Fault{Method: c.method, Path: c.path, Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN"}`)})
		_, err := Converge(t.Context(), w.admin, &recorder{}, w.team, ids)
		e, ok := errors.AsType[*Error](err)
		if !ok || e.Step != c.step || e.Purpose != proto.PurposeMain || e.Drift || !errors.Is(err, matrix.ErrForbidden) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestConvergeIntentFailureStopsTheCreate(t *testing.T) {
	w := newWorld(t)
	_, err := Converge(t.Context(), w.admin, &recorder{fail: "intent main"}, w.team, proto.RoomIDs{})
	if e, ok := errors.AsType[*Error](err); !ok || e.Step != StepCreate || e.Drift {
		t.Fatalf("intent failure = %v", err)
	}
	if created := w.adminCreated(t); len(created) != 0 {
		t.Fatalf("a room was created without its intent: %v", created)
	}
}

func TestErrorText(t *testing.T) {
	cause := errors.New("boom")
	err := &Error{Purpose: proto.PurposeReports, Step: StepKick, Err: cause}
	if err.Error() != "room reports, step kick: boom" || !errors.Is(err, cause) {
		t.Fatalf("error %q", err)
	}
}
