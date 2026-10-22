package view

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

const (
	server = "alpha.teamchat.internal"
	coord  = "myrepo.1+desk.podman"
	worker = "myrepo.2+desk.podman"
	pw     = "pw"
)

func user(local string) string { return proto.UserID(local, server) }

func fixedNow() time.Time { return time.UnixMilli(1_760_000_000_000).UTC() }

type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func alphaID(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// teamDoc is alpha's input: humans alice and bob, coordinator coord.
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

type approvals map[string]*declare.Gated

func (a approvals) Load(id paths.TeamID) (*declare.Gated, error) { return a[id.String()], nil }

// world is alpha applied on fakehs, with coord and worker active members who joined their
// rooms on their seats' devices, and the three files on disk.
type world struct {
	srv   *fakehs.Server
	admin *matrix.Admin
	dir   string
	in    declare.Input
	rooms proto.RoomIDs
}

func newServer(t *testing.T) (*fakehs.Server, *matrix.Admin) {
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
	srv.AddUser(proto.AdminLocalpart, pw, true)
	token, err := srv.MintToken(proto.AdminID(server))
	if err != nil {
		t.Fatal(err)
	}
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: srv.URL(), Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return srv, a
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv, a := newServer(t)
	for _, h := range []string{coord, worker} {
		srv.AddUser(h, pw, false)
	}
	raw, err := json.Marshal(teamDoc())
	if err != nil {
		t.Fatal(err)
	}
	in, err := declare.DecodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	g, err := declare.Approve(nil, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{srv: srv, admin: a, dir: t.TempDir(), in: in}
	deps := apply.Deps{Admin: a, Approvals: approvals{"alpha": &g}, Now: fixedNow, RosterPath: w.path(rosterName), RoomsPath: w.path(roomsName)}
	if _, err = apply.Run(t.Context(), deps, in); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	w.editRoster(t, func(f *apply.RosterFile) {
		f.Members[coord] = apply.Member{State: apply.MemberActive, Kind: "podman", HumanText: true, Cred: "password"}
		f.Members[worker] = apply.Member{State: apply.MemberActive, Kind: "podman", HumanText: true, Cred: "password"}
	})
	res, err := apply.Run(t.Context(), deps, in)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	w.rooms = res.Rooms
	if err := os.WriteFile(w.path(teamName), res.TeamFile, 0o600); err != nil {
		t.Fatal(err)
	}
	w.join(t, coord, w.rooms.Main, w.rooms.Reports, w.rooms.Questions)
	w.join(t, worker, w.rooms.Main, w.rooms.Reports)
	return w
}

const (
	teamName   = "team.json"
	rosterName = "roster.json"
	roomsName  = "rooms.json"
)

func (w *world) path(name string) string { return filepath.Join(w.dir, name) }

func (w *world) editRoster(t *testing.T, edit func(*apply.RosterFile)) {
	t.Helper()
	raw, err := os.ReadFile(w.path(rosterName))
	if err != nil {
		t.Fatal(err)
	}
	f, err := apply.DecodeRosterFile(raw, alphaID(t))
	if err != nil {
		t.Fatal(err)
	}
	edit(&f)
	f.Seq++
	if err := state.WriteFile(w.path(rosterName), f.Encode(), 0o600, -1); err != nil {
		t.Fatal(err)
	}
}

// seat is handle signed in on its seat's device.
func (w *world) seat(t *testing.T, handle string) *matrix.Admin {
	t.Helper()
	sess, err := w.admin.LoginDevice(t.Context(), handle, pw, paths.SeatDevice)
	if err != nil {
		t.Fatalf("LoginDevice %s: %v", handle, err)
	}
	c, err := w.admin.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *world) join(t *testing.T, handle string, rooms ...string) {
	t.Helper()
	c := w.seat(t, handle)
	for _, room := range rooms {
		if _, err := c.Join(t.Context(), room); err != nil {
			t.Fatalf("%s joins %s: %v", handle, room, err)
		}
	}
}

func (w *world) input(t *testing.T) Input {
	t.Helper()
	in, err := Load(alphaID(t), w.path(teamName), w.path(rosterName), w.path(roomsName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return in
}

func (w *world) read(t *testing.T) health.View {
	t.Helper()
	mark := len(w.srv.Requests())
	v, err := Read(t.Context(), w.admin, w.input(t))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, r := range w.srv.Requests()[mark:] {
		if r.Method != http.MethodGet {
			t.Fatalf("the view wrote: %s %s", r.Method, r.Path)
		}
	}
	return v
}

func whats(ps []status.Problem) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.What+" | "+p.Fix)
	}
	return out
}

func wantProblems(t *testing.T, v health.View, want ...string) {
	t.Helper()
	if got := whats(v.Problems); !slices.Equal(got, want) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, p := range v.Problems {
		if _, err := status.NewProblem(p.What, p.Fix); err != nil {
			t.Fatalf("problem %q is not two lines: %v", p.What, err)
		}
	}
}

// VW1, VW2: a converged team has no problem, and its memberships and humans are read.
func TestAConvergedTeamHasNoProblem(t *testing.T) {
	w := newWorld(t)
	if _, err := w.srv.MintToken(user("alice")); err != nil {
		t.Fatal(err)
	}
	v := w.read(t)
	wantProblems(t, v)
	want := []status.Member{
		{Handle: coord, Kind: "podman", Role: proto.RoleCoordinator, State: apply.MemberActive, Rooms: map[string]string{"main": "join", "reports": "join", "questions": "join"}},
		{Handle: worker, Kind: "podman", Role: proto.RoleWorker, State: apply.MemberActive, Rooms: map[string]string{"main": "join", "reports": "join"}},
	}
	wantMembers(t, v.Members, want)
	if len(v.Humans) != 2 || v.Humans[0].Name != "alice" || v.Humans[1].Name != "bob" {
		t.Fatalf("humans %+v", v.Humans)
	}
	alice, bob := v.Humans[0], v.Humans[1]
	if alice.Devices != 1 || bob.Devices != 0 || alice.State != apply.HumanActive {
		t.Fatalf("devices: alice %+v, bob %+v", alice, bob)
	}
	if alice.Rooms["main"] != "invite" || alice.Rooms["questions"] != "invite" {
		t.Fatalf("alice's rooms %v", alice.Rooms)
	}
}

func wantMembers(t *testing.T, got, want []status.Member) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("members %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Handle != w.Handle || g.Kind != w.Kind || g.Role != w.Role || g.State != w.State || g.Online != nil || !maps.Equal(g.Rooms, w.Rooms) {
			t.Fatalf("member %d: %+v, want %+v", i, g, w)
		}
	}
}

// VW2: the newest last_seen_ts of a human's devices.
func TestAHumansNewestSignIn(t *testing.T) {
	devices := []matrix.Device{{ID: "A", LastSeen: time.UnixMilli(5)}, {ID: "B", LastSeen: time.UnixMilli(9)}, {ID: "C"}}
	if n, at := signIns(devices); n != 3 || at != 9 {
		t.Fatalf("signIns: %d, %d", n, at)
	}
	if n, at := signIns(nil); n != 0 || at != 0 {
		t.Fatalf("no devices: %d, %d", n, at)
	}
}

// VW3: a device other than the seat's.
func TestAForeignDeviceIsAProblem(t *testing.T) {
	w := newWorld(t)
	if _, err := w.srv.MintToken(user(worker)); err != nil {
		t.Fatal(err)
	}
	wantProblems(t, w.read(t), worker+" has a device that is not its seat's | ccy-teams reissue "+worker)
}

// VW3: a deactivated account, the coordinator's fix naming another coordinator first.
func TestADeactivatedAccountIsAProblem(t *testing.T) {
	w := newWorld(t)
	for _, h := range []string{coord, worker} {
		if err := w.admin.SetDeactivated(t.Context(), user(h), true); err != nil {
			t.Fatal(err)
		}
	}
	wantProblems(t, w.read(t),
		coord+" is deactivated | name another coordinator in the declaration, then teamchat-admin member retire "+coord+" and add a new member",
		worker+" is deactivated | teamchat-admin member retire "+worker+" and add a new member",
	)
}

// VW3: a member with no account; a parked one is watched, a retired one is not.
func TestAMissingAccountIsAProblem(t *testing.T) {
	w := newWorld(t)
	gone, retired := "myrepo.3+desk.podman", "myrepo.4+desk.podman"
	w.editRoster(t, func(f *apply.RosterFile) {
		f.Members[gone] = apply.Member{State: apply.MemberParked, Kind: "podman", HumanText: true, Cred: "password"}
		f.Members[retired] = apply.Member{State: apply.MemberRetired, Kind: "podman", HumanText: true, Cred: "password"}
	})
	v := w.read(t)
	if !slices.Contains(whats(v.Problems), gone+" has no account | ccy-teams reissue "+gone) {
		t.Fatalf("problems %v", whats(v.Problems))
	}
	for _, p := range v.Problems {
		if strings.HasPrefix(p.What, retired) {
			t.Fatalf("a retired member is watched: %v", p)
		}
	}
}

// VW4: a state event that differs is drift, read with nothing written.
func TestDriftIsAProblemAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.PutState(t.Context(), w.rooms.Main, "m.room.topic", "", json.RawMessage(`{"topic":"changed"}`)); err != nil {
		t.Fatal(err)
	}
	wantProblems(t, w.read(t), "room main: state m.room.topic differs | ccy-teams apply alpha")
}

// VW4: a room the engine would create is one problem.
func TestAMissingRoomIsAProblem(t *testing.T) {
	srv, a := newServer(t)
	srv.AddUser("alice", pw, false)
	team, err := declare.DecodeTeamFile(newWorld(t).teamFile(t))
	if err != nil {
		t.Fatal(err)
	}
	f := apply.NewRosterFile(alphaID(t))
	f.Humans["alice"] = apply.Human{State: apply.HumanActive}
	v, err := Read(t.Context(), a, Input{Team: team, Roster: f})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	wantProblems(t, v, "room main does not exist | ccy-teams apply alpha")
}

func (w *world) teamFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(w.path(teamName))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// VW4: drift the engine refuses names its room and step.
func TestRefusedDriftNamesItsRoomAndStep(t *testing.T) {
	w := newWorld(t)
	c := w.seat(t, coord)
	other, err := c.CreateRoom(t.Context(), matrix.CreateRoomRequest{Preset: "private_chat", RoomVersion: proto.RoomVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Invite(t.Context(), other, proto.AdminID(server)); err != nil {
		t.Fatal(err)
	}
	if _, err = w.admin.Join(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	in := w.input(t)
	in.Rooms.Questions = other
	v, err := Read(t.Context(), w.admin, in)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := whats(v.Problems); !slices.Contains(got, "room questions: drift the rooms engine will not repair (step read) | ccy-teams apply alpha names it") {
		t.Fatalf("problems %v", got)
	}
}

// VW5: a worker in the questions room.
func TestAWorkerInTheQuestionsRoomIsAProblem(t *testing.T) {
	w := newWorld(t)
	if err := w.admin.Invite(t.Context(), w.rooms.Questions, user(worker)); err != nil {
		t.Fatal(err)
	}
	v := w.read(t)
	got := whats(v.Problems)
	if !slices.Contains(got, "the worker "+worker+" is in the questions room | ccy-teams apply alpha") ||
		!slices.Contains(got, "room questions: would kick "+user(worker)+" | ccy-teams apply alpha") {
		t.Fatalf("problems %v", got)
	}
}

// VW5: the open coordinator not joined.
func TestTheCoordinatorNotJoinedIsAProblem(t *testing.T) {
	w := newWorld(t)
	if err := w.seat(t, coord).Leave(t.Context(), w.rooms.Questions); err != nil {
		t.Fatal(err)
	}
	got := whats(w.read(t).Problems)
	if !slices.Contains(got, "the coordinator "+coord+" has not joined the questions room | ccy-teams restart "+coord) {
		t.Fatalf("problems %v", got)
	}
}

// VW5: the questions room's form is not the roster's.
func TestTheQuestionsRoomsFormIsChecked(t *testing.T) {
	w := newWorld(t)
	w.editRoster(t, func(f *apply.RosterFile) {
		m := f.Members[coord]
		m.State = apply.MemberParked
		f.Members[coord] = m
	})
	got := whats(w.read(t).Problems)
	if !slices.Contains(got, "the questions room is not closed, as the roster says it is | ccy-teams apply alpha") {
		t.Fatalf("problems %v", got)
	}
}

// VW6: a pending operation and the verb that rolls it forward.
func TestAPendingOperationIsAProblem(t *testing.T) {
	for _, c := range []struct {
		pending apply.Pending
		want    string
	}{
		{apply.Pending{Op: apply.OpTeamApply, Step: "rooms"}, "team.apply did not finish (step rooms) | ccy-teams apply alpha"},
		{apply.Pending{Op: "member.add", Step: "account", Handle: worker}, "member.add did not finish (step account) | teamchat-admin member add " + worker},
		{apply.Pending{Op: "team.restore", Step: "swap"}, "team.restore did not finish (step swap) | teamchat-admin team restore alpha"},
	} {
		got := pendingProblem(alphaID(t), &c.pending)
		if line := got.What + " | " + got.Fix; line != c.want {
			t.Fatalf("pending %+v: %q, want %q", c.pending, line, c.want)
		}
	}
	if p := pendingProblem(alphaID(t), &apply.Pending{Op: "team.x" + string(rune(0x200b)), Step: "s"}); p.What != "an operation did not finish" {
		t.Fatalf("an op that is not one line: %+v", p)
	}
	w := newWorld(t)
	w.editRoster(t, func(f *apply.RosterFile) { f.Pending = &apply.Pending{Op: apply.OpTeamApply, Step: "rooms"} })
	if got := whats(w.read(t).Problems); !slices.Contains(got, "team.apply did not finish (step rooms) | ccy-teams apply alpha") {
		t.Fatalf("problems %v", got)
	}
}

// A homeserver that fails is the view's error, not a problem.
func TestAFailingHomeserverIsAnError(t *testing.T) {
	for _, path := range []string{"/state", "/devices", "/v2/users/"} {
		w := newWorld(t)
		in := w.input(t)
		w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: path, Status: http.StatusInternalServerError, Times: 100})
		if _, err := Read(t.Context(), w.admin, in); err == nil {
			t.Fatalf("%s failing: no error", path)
		}
	}
}

// Load reads the three files strictly; a missing roster or rooms record is empty.
func TestLoad(t *testing.T) {
	w := newWorld(t)
	in := w.input(t)
	if in.Rooms != w.rooms || len(in.Roster.Members) != 2 || in.Team.ID != alphaID(t) {
		t.Fatalf("Load: %+v", in)
	}
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if _, err := Load(alphaID(t), missing, w.path(rosterName), w.path(roomsName)); !errors.Is(err, health.ErrViewFiles) {
		t.Fatalf("a missing team.json: %v, want health.ErrViewFiles", err)
	}
	in, err := Load(alphaID(t), w.path(teamName), missing, missing)
	if err != nil || len(in.Roster.Members) != 0 || in.Rooms != (proto.RoomIDs{}) {
		t.Fatalf("missing files: %+v, %v", in, err)
	}
	bad := filepath.Join(dir, "bad")
	if err = os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, files := range [][3]string{{bad, missing, missing}, {w.path(teamName), bad, missing}, {w.path(teamName), missing, bad}} {
		if _, err = Load(alphaID(t), files[0], files[1], files[2]); !errors.Is(err, health.ErrViewFiles) {
			t.Fatalf("%v: %v, want health.ErrViewFiles", files, err)
		}
	}
	beta, err := paths.ParseTeamID("beta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(beta, w.path(teamName), missing, missing); !errors.Is(err, ErrOtherTeam) || !errors.Is(err, health.ErrViewFiles) {
		t.Fatalf("another team's team.json: %v", err)
	}
}
