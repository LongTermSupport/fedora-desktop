package apply

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const (
	server = "alpha.teamchat.internal"
	coord  = "myrepo.1+desk.podman"
	other  = "myrepo.2+desk.podman"
)

func user(local string) string { return proto.UserID(local, server) }

func fixedNow() time.Time { return time.UnixMilli(1_760_000_000_000).UTC() }

type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// teamDoc is alpha's one-team input: humans alice and bob, coordinator coord.
func teamDoc() map[string]any {
	return map[string]any{
		"team": map[string]any{
			"team": "alpha", "state": "present", "port": 8448, "description": "The team that builds the thing.",
			"listen": []any{"192.0.2.10"}, "allow_from": []any{"192.0.2.0/24"}, "humans": []any{"alice", "bob"},
			"coordinator": coord,
			"trust": map[string]any{
				"kind": "github", "org": "example-org",
				"repos": []any{map[string]any{"repo": "example-org/thing", "branches": []any{"main"}}},
			},
		},
		"lxc_members": []any{},
		"address":     "192.0.2.10",
	}
}

func docTeam(doc map[string]any) map[string]any {
	m, ok := doc["team"].(map[string]any)
	if !ok {
		panic("fixture: no team")
	}
	return m
}

func input(t *testing.T, doc map[string]any) declare.Input {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	in, err := declare.DecodeInput(raw)
	if err != nil {
		t.Fatalf("DecodeInput: %v", err)
	}
	return in
}

// approvals is an in-memory approval store; a team never approved is a nil entry.
type approvals map[string]*declare.Gated

func (a approvals) Load(id paths.TeamID) (*declare.Gated, error) { return a[id.String()], nil }

// world is alpha's homeserver, its admin client and the team's files.
type world struct {
	srv      *fakehs.Server
	admin    *matrix.Admin
	approved approvals
	dir      string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv := fakehs.New(fakehs.Options{ServerName: server, Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
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
	for _, h := range []string{coord, other} {
		srv.AddUser(h, "pw", false)
	}
	token, err := srv.MintToken(proto.AdminID(server))
	if err != nil {
		t.Fatal(err)
	}
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return &world{srv: srv, admin: a, approved: approvals{}, dir: t.TempDir()}
}

func (w *world) deps() Deps {
	return Deps{
		Admin: w.admin, Approvals: w.approved, Now: fixedNow,
		RosterPath: filepath.Join(w.dir, "roster.json"), RoomsPath: filepath.Join(w.dir, "rooms.json"),
	}
}

// approve approves the input whole, as `team approve` with no --only would.
func (w *world) approve(t *testing.T, in declare.Input) {
	t.Helper()
	g, err := declare.Approve(nil, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.approved[in.ID().String()] = &g
}

func (w *world) run(t *testing.T, in declare.Input) Result {
	t.Helper()
	res, err := Run(t.Context(), w.deps(), in)
	if err != nil {
		t.Fatalf("Run: %v\nchanged: %v", err, res.Changed)
	}
	return res
}

// applied is a run over an approved input.
func (w *world) applied(t *testing.T, doc map[string]any) Result {
	t.Helper()
	in := input(t, doc)
	w.approve(t, in)
	return w.run(t, in)
}

func (w *world) file(t *testing.T) *store {
	t.Helper()
	s := &store{team: alphaID(), rosterPath: w.deps().RosterPath, roomsPath: w.deps().RoomsPath}
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	return s
}

// writes are the requests since mark that change something.
func (w *world) writes(mark int) []string {
	var out []string
	for _, r := range w.srv.Requests()[mark:] {
		if r.Method != http.MethodGet {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

func (w *world) mark() int { return len(w.srv.Requests()) }

// addMember puts handle in the roster file as `member add` would have.
func (w *world) addMember(t *testing.T, handle string, m Member) {
	t.Helper()
	s := w.file(t)
	s.roster.Members[handle] = m
	if err := s.saveRoster(); err != nil {
		t.Fatal(err)
	}
}

func (w *world) roster(t *testing.T, ids proto.RoomIDs, self string) proto.Roster {
	t.Helper()
	raw, err := w.admin.RoomState(t.Context(), ids.Main)
	if err != nil {
		t.Fatal(err)
	}
	r, err := proto.VerifyMain(parseState(t, raw), proto.Anchors{Server: server, Team: "alpha", MainRoom: ids.Main, Self: self})
	if err != nil {
		t.Fatalf("VerifyMain: %v", err)
	}
	return r
}

func parseState(t *testing.T, raw []json.RawMessage) []proto.StateEvent {
	t.Helper()
	out := make([]proto.StateEvent, 0, len(raw))
	for _, r := range raw {
		v, err := proto.Parse(r)
		if err != nil {
			t.Fatal(err)
		}
		get := func(k string) proto.Value {
			f, found := v.Get(k)
			if !found {
				t.Fatalf("a state event without %s", k)
			}
			return f
		}
		out = append(out, proto.StateEvent{
			Type: get("type").Str(), StateKey: get("state_key").Str(), Sender: get("sender").Str(),
			EventID: get("event_id").Str(), Content: get("content"),
		})
	}
	return out
}

func active() Member {
	return Member{State: MemberActive, Kind: "podman", HumanText: true, Cred: credPassword}
}

func TestANewTeam(t *testing.T) {
	w := newWorld(t)
	in := input(t, teamDoc())
	w.approve(t, in)
	res := w.run(t, in)
	if !slices.Equal(res.Changed[:2], []string{"human alice created", "human bob created"}) ||
		!slices.Contains(res.Changed, "room main state teamchat.roster") {
		t.Fatalf("changed %v", res.Changed)
	}
	if string(res.TeamFile) != string(in.Team.Canonical()) {
		t.Fatalf("team.json %s", res.TeamFile)
	}
	s := w.file(t)
	if s.rooms != res.Rooms || s.rooms.Questions == "" || s.roster.Rev != 1 || s.roster.Pending != nil ||
		s.roster.CoordinatorWanted != coord || s.roster.Coordinator != "" {
		t.Fatalf("files %+v %+v", s.roster, s.rooms)
	}
	alice := s.roster.Humans["alice"]
	if alice.State != HumanActive || alice.RevokedSeq == 0 || alice.SinceMS != fixedNow().UnixMilli() {
		t.Fatalf("alice %+v", alice)
	}
	w.plainAccounts(t, "alice", "bob")
}

// plainAccounts checks that each human has an account that is neither an admin nor locked.
func (w *world) plainAccounts(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		acct, found, err := w.admin.Account(t.Context(), user(name))
		if err != nil || !found || acct.Admin || acct.Locked {
			t.Fatalf("%s: %+v %v %v", name, acct, found, err)
		}
	}
}

// AP10: the second run over an applied team writes nothing and says nothing.
func TestASecondRunChangesNothing(t *testing.T) {
	w := newWorld(t)
	in := input(t, teamDoc())
	w.approve(t, in)
	w.run(t, in)
	mark, seq := w.mark(), w.file(t).roster.Seq
	again := w.run(t, in)
	if len(again.Changed) != 0 || len(w.writes(mark)) != 0 || w.file(t).roster.Seq != seq {
		t.Fatalf("second run: %v, writes %v, seq %d -> %d", again.Changed, w.writes(mark), seq, w.file(t).roster.Seq)
	}
}

func TestAWantedCoordinatorIsPromotedOnceAMember(t *testing.T) {
	w := newWorld(t)
	w.applied(t, teamDoc())
	w.addMember(t, coord, active())
	w.addMember(t, other, active())
	res := w.applied(t, teamDoc())
	if !slices.Contains(res.Changed, "role "+coord+" promoted") || !slices.Equal(res.Roles, []RoleChange{{coord, RolePromoted}}) {
		t.Fatalf("changed %v, roles %v", res.Changed, res.Roles)
	}
	s := w.file(t)
	if s.roster.Coordinator != coord || s.roster.CoordinatorWanted != "" {
		t.Fatalf("roster file %+v", s.roster)
	}
	r := w.roster(t, s.rooms, user(other))
	if r.Members[user(coord)].Role != proto.RoleCoordinator || r.Members[user(other)].Role != proto.RoleWorker ||
		r.Rev != s.roster.Rev || !slices.Equal(r.Humans, []string{user("alice"), user("bob")}) {
		t.Fatalf("roster event %+v", r)
	}
}

func TestACoordinatorChangeDemotesFirst(t *testing.T) {
	w := newWorld(t)
	w.applied(t, teamDoc())
	w.addMember(t, coord, active())
	w.addMember(t, other, active())
	w.applied(t, teamDoc())
	doc := teamDoc()
	docTeam(doc)["coordinator"] = other
	res := w.applied(t, doc)
	demoted, promoted := slices.Index(res.Changed, "role "+coord+" demoted"), slices.Index(res.Changed, "role "+other+" promoted")
	if demoted < 0 || promoted < demoted || !slices.Contains(res.Changed[demoted:promoted], "room main state teamchat.roster") {
		t.Fatalf("changed %v", res.Changed)
	}
	if !slices.Equal(res.Roles, []RoleChange{{coord, RoleDemoted}, {other, RolePromoted}}) {
		t.Fatalf("roles %v", res.Roles)
	}
	// No coordinator declared clears the role.
	doc = teamDoc()
	delete(docTeam(doc), "coordinator")
	res = w.applied(t, doc)
	if !slices.Equal(res.Roles, []RoleChange{{other, RoleDemoted}}) || w.file(t).roster.Coordinator != "" {
		t.Fatalf("no coordinator: %v", res.Roles)
	}
}

func TestAHumanRemovedIsKickedThenLockedAndAddedBackUnlocked(t *testing.T) {
	w := newWorld(t)
	w.applied(t, teamDoc())
	doc := teamDoc()
	docTeam(doc)["humans"] = []any{"alice"}
	res := w.applied(t, doc)
	kicked := slices.Index(res.Changed, "room main kicked "+user("bob"))
	locked := slices.Index(res.Changed, "human bob locked")
	if kicked < 0 || locked != len(res.Changed)-1 {
		t.Fatalf("changed %v", res.Changed)
	}
	if acct, _, err := w.admin.Account(t.Context(), user("bob")); err != nil || !acct.Locked {
		t.Fatalf("bob %+v %v", acct, err)
	}
	if w.file(t).roster.Humans["bob"].State != HumanRemoved {
		t.Fatal("bob is not removed in the file")
	}
	mark := w.mark()
	if again := w.applied(t, doc); len(again.Changed) != 0 || len(w.writes(mark)) != 0 {
		t.Fatalf("a removed human's second run: %v %v", again.Changed, w.writes(mark))
	}
	res = w.applied(t, teamDoc())
	if res.Changed[0] != "human bob unlocked" || !slices.Contains(res.Changed, "room main invited "+user("bob")) {
		t.Fatalf("added back %v", res.Changed)
	}
	if acct, _, err := w.admin.Account(t.Context(), user("bob")); err != nil || acct.Locked {
		t.Fatalf("bob %+v %v", acct, err)
	}
}

func TestADeclarationNotApprovedIsRefusedBeforeAnyCall(t *testing.T) {
	w := newWorld(t)
	w.applied(t, teamDoc())
	doc := teamDoc()
	docTeam(doc)["humans"] = []any{"alice", "bob", "mallory"}
	mark := w.mark()
	_, err := Run(t.Context(), w.deps(), input(t, doc))
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Step != StepGate || !e.Drift {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "SECURITY\talpha\thumans\t- -> mallory") || w.mark() != mark {
		t.Fatalf("refusal %v after %d requests", err, w.mark()-mark)
	}
	if _, err := Run(t.Context(), Deps{Approvals: failing{}}, input(t, doc)); err == nil {
		t.Fatal("an unreadable approval passed")
	}
}

type failing struct{}

func (failing) Load(paths.TeamID) (*declare.Gated, error) { return nil, errors.New("unreadable") }

func TestRefusalsBeforeAnyChange(t *testing.T) {
	cases := map[string]struct {
		step  string
		setup func(t *testing.T, w *world)
	}{
		"another server admin": {StepAdmins, func(_ *testing.T, w *world) { w.srv.AddUser("root", "pw", true) }},
		"a deactivated human": {StepHumans, func(t *testing.T, w *world) {
			t.Helper()
			w.srv.AddUser("bob", "pw", false)
			if err := w.admin.SetDeactivated(t.Context(), user("bob"), true); err != nil {
				t.Fatal(err)
			}
		}},
		"a retired coordinator": {StepRoles, func(t *testing.T, w *world) {
			t.Helper()
			m := active()
			m.State = MemberRetired
			w.addMember(t, coord, m)
		}},
		"a coordinator without human text": {StepRoles, func(t *testing.T, w *world) {
			t.Helper()
			m := active()
			m.HumanText = false
			w.addMember(t, coord, m)
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.setup(t, w)
			in := input(t, teamDoc())
			w.approve(t, in)
			mark := w.mark()
			res, err := Run(t.Context(), w.deps(), in)
			e, ok := errors.AsType[*Error](err)
			if !ok || e.Step != c.step || !e.Drift {
				t.Fatalf("err = %v", err)
			}
			if len(res.Changed) != 0 || len(w.writes(mark)) != 0 {
				t.Fatalf("changed %v, writes %v", res.Changed, w.writes(mark))
			}
		})
	}
}

func TestAFailureKeepsWhatChangedBeforeIt(t *testing.T) {
	w := newWorld(t)
	in := input(t, teamDoc())
	w.approve(t, in)
	w.srv.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/invite", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN"}`)})
	res, err := Run(t.Context(), w.deps(), in)
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Step != StepRooms || e.Drift {
		t.Fatalf("err = %v", err)
	}
	if !slices.Contains(res.Changed, "human alice created") || !slices.Contains(res.Changed, "room main state teamchat.roster") {
		t.Fatalf("changed %v", res.Changed)
	}
	s := w.file(t)
	if s.roster.Humans["alice"].State != HumanActive || s.rooms.Main == "" || s.roster.Rev != 1 {
		t.Fatalf("files after the failure %+v %+v", s.roster, s.rooms)
	}
	again := w.run(t, in)
	if slices.Contains(again.Changed, "human alice created") || w.file(t).roster.Rev != 1 {
		t.Fatalf("the rerun %v", again.Changed)
	}
}

func TestAsApproved(t *testing.T) {
	in := input(t, teamDoc())
	g := in.Gated()
	g.Trust.Org = "approved-org"
	g.URL = "https://chat.example.com"
	g.Coordinator = other
	team := asApproved(in.Team, g)
	if team.Trust.Org != "approved-org" || team.URL != g.URL || team.Coordinator != other || in.Team.Trust.Org != "example-org" {
		t.Fatalf("as approved %+v", team)
	}
}

func TestDeriveRoster(t *testing.T) {
	team := input(t, teamDoc()).Team
	f := NewRosterFile(alphaID())
	f.Rev, f.RestoreEpoch, f.Coordinator = 4, 2, coord
	f.Humans["alice"] = Human{State: HumanActive}
	f.Humans["bob"] = Human{State: HumanRemoved}
	f.Humans["carol"] = Human{State: HumanActive}
	parked := active()
	parked.State = MemberParked
	f.Members[coord] = parked
	f.Members[other] = active()
	for i, s := range []string{MemberRetired, MemberLost, MemberClaimed} {
		m := active()
		m.State = s
		f.Members["gone."+string(rune('1'+i))+"+desk.podman"] = m
	}
	r, err := DeriveRoster(f, team)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]proto.RosterMember{
		user(coord): {Role: proto.RoleCoordinator, HumanText: true, Parked: true},
		user(other): {Role: proto.RoleWorker, HumanText: true},
	}
	if r.Team != "alpha" || r.Name != "alpha" || r.Rev != 4 || r.RestoreEpoch != 2 || len(r.Members) != 2 ||
		r.Members[user(coord)] != want[user(coord)] || r.Members[user(other)] != want[user(other)] ||
		!slices.Equal(r.Humans, []string{user("alice")}) || r.Forge.Org != "example-org" || r.Limits != proto.DefaultLimits() {
		t.Fatalf("derived %+v", r)
	}
	f.Humans["alice"] = Human{State: HumanRemoved}
	if _, err := DeriveRoster(f, team); err == nil {
		t.Fatal("a roster with no human was accepted")
	}
}
