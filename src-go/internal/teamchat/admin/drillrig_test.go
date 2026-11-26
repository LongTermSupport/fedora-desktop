package admin

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// drillPort is the declared drill port the tests apply the scratch team on.
const drillPort = 8470

// plainBundles makes a bundle of any team, as the seat side would.
type plainBundles struct{}

func (plainBundles) Bundle(_ context.Context, team declare.Team, handle, _ string, c apply.Credential, _ Reach) (proto.Value, error) {
	return proto.Obj(proto.M("team", proto.Str(team.ID.String())), proto.M("handle", proto.Str(handle)), proto.M("kind", proto.Str(c.Kind))), nil
}

// rigWorld is the admin world with the drill's rig over it: the scratch team's homeserver is
// fakehs, and every engine step is a recorded fake.
type rigWorld struct {
	*world

	rig     *DrillRig
	calls   []string
	held    []string
	backups int64
	err     map[string]error
}

func newRigWorld(t *testing.T) *rigWorld {
	t.Helper()
	w := &rigWorld{world: scratchWorld(t), err: map[string]error{}}
	sys := w.sys()
	sys.Bundles = plainBundles{}
	sys.Host.Network = scratchNetwork{w.world}
	w.rig = &DrillRig{
		Sys:    sys,
		Engine: restore.Deps{Root: w.root, StateOwnerUID: selfUID(t), Child: epochChild{epoch: 4}},
		RunRestore: func(ctx context.Context, d restore.Deps, held *teamlock.Lock, req restore.Request) (restore.Answer, error) {
			w.calls = append(w.calls, "restore "+req.Team.String())
			w.held = append(w.held, held.Team().String())
			if d.OnStaged != nil {
				if err := d.OnStaged(ctx, req.Team); err != nil {
					return restore.Answer{}, err
				}
			}
			if d.AfterSwap != nil {
				if err := d.AfterSwap(ctx, req.Team); err != nil {
					return restore.Answer{}, err
				}
			}
			return restore.Answer{}, w.err["restore"]
		},
		RunDiscard: func(_ context.Context, _ restore.Deps, team paths.TeamID) (restore.Answer, error) {
			w.calls = append(w.calls, "discard "+team.String())
			return restore.Answer{}, w.err["discard"]
		},
		TakeBackup: func(_ context.Context, held *teamlock.Lock) (int64, error) {
			w.backups++
			w.held = append(w.held, held.Team().String())
			return w.backups, w.err["backup"]
		},
		Base:  func(int) string { return w.srv.URL() },
		Clock: w,
	}
	return w
}

// epochChild is the restore child as the drill asks it: restore.check of the live server answers
// epoch; nothing else is asked.
type epochChild struct {
	restore.Child

	epoch int64
}

func (c epochChild) Check(_ context.Context, _ paths.TeamID, unit []string, a restore.CheckArgs) (restore.Epoch, error) {
	if unit != nil || !a.Live() || len(a.TeamFile) == 0 {
		return restore.Epoch{}, errors.New("not the live check with the team file")
	}
	return restore.Epoch{RestoreEpoch: c.epoch, Rev: 1}, nil
}

// scratchWorld is the host tree with the scratch team's homeserver on fakehs, its admin account
// made as the install's bootstrap would.
func scratchWorld(t *testing.T) *world {
	t.Helper()
	w := bareWorld(t)
	name := paths.ServerName(scratch(t))
	w.srv = fakehs.New(fakehs.Options{ServerName: name, Now: w.now, Quirks: fakehs.TuwunelQuirks()})
	if err := w.srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := w.srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	w.srv.AddUser(proto.AdminLocalpart, "pw", true)
	token, err := w.srv.MintToken(proto.AdminID(name))
	if err != nil {
		t.Fatal(err)
	}
	if w.admin, err = matrix.NewAdmin(matrix.Config{BaseURL: w.srv.URL(), Token: token, Clock: w}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.admin.Close)
	return w
}

// scratchNetwork is the world's network with the scratch team's homeserver listening on
// loopback on the drill port while its unit is active.
type scratchNetwork struct{ *world }

func (n scratchNetwork) Listeners() ([]host.Listener, error) {
	unit := paths.ServerUnit(scratch(n.t))
	if u := n.units[unit]; u != nil && u.Active == "active" {
		return []host.Listener{{Addr: netip.MustParseAddrPort("127.0.0.1:8470"), Unit: unit}}, nil
	}
	return nil, nil
}

func scratch(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("rd-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seeded is the scratch team applied on the drill port and seeded.
func (w *rigWorld) seeded(t *testing.T) (paths.TeamID, drill.Seed) {
	t.Helper()
	id := scratch(t)
	if err := w.rig.Apply(t.Context(), id, drillPort); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	s, err := w.rig.Seed(t.Context(), id)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return id, s
}

// The scratch input is a present team on the drill port, loopback only: no network, no LXC
// member, no address, and a URL no member resolves; its one human is the drill's.
func TestDrillInputIsALoopbackOnlyScratchTeam(t *testing.T) {
	in, err := DrillInput(scratch(t), drillPort)
	if err != nil {
		t.Fatal(err)
	}
	tm := in.Team
	if tm.ID != scratch(t) || tm.Port != drillPort || len(tm.Networks) != 0 || len(in.LXC) != 0 || in.Address != "" ||
		!strings.HasSuffix(tm.URL, ".invalid:8470") || !slices.Equal(tm.Humans, []string{drillHuman}) {
		t.Errorf("DrillInput = %+v", in)
	}
}

// Apply approves the scratch input in process and applies it through host.Apply with the
// child's bootstrap: the team's files are written and its homeserver unit started.
func TestDrillRigAppliesTheScratchTeam(t *testing.T) {
	w := newRigWorld(t)
	id := scratch(t)
	if err := w.rig.Apply(t.Context(), id, drillPort); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{paths.ApprovedFile(id), paths.TeamDeclaration(id), paths.RoomsFile(id)} {
		if _, err := os.Stat(w.root + p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if u := w.units[paths.ServerUnit(id)]; u == nil || u.Active != "active" {
		t.Errorf("the scratch homeserver is not active: %+v", u)
	}
}

// Seed adds two token members and resets the drill human's password through the child, then
// posts messages and each token member's floor marker on the team's own server: each credential
// it answers is accepted there.
func TestDrillRigSeedsMembersAHumanMessagesAndFloors(t *testing.T) {
	w := newRigWorld(t)
	id, s := w.seeded(t)
	if len(s.Events) != 2 || s.Kept.Handle == s.Reissued.Handle || s.Kept.Secret == "" || s.Human.Secret == "" {
		t.Fatalf("Seed = %d events, kept %s, reissued %s", len(s.Events), s.Kept.Handle, s.Reissued.Handle)
	}
	for _, c := range []drill.Credential{s.Kept, s.Reissued, s.Human} {
		ok, err := w.rig.Accepts(t.Context(), id, c)
		if err != nil || !ok {
			t.Errorf("Accepts(%s) = %v, %v", c.Handle, ok, err)
		}
	}
	missing, err := w.rig.MissingEvents(t.Context(), id, s.Events)
	if err != nil || len(missing) != 0 {
		t.Errorf("MissingEvents = %q, %v", missing, err)
	}
	floors, err := w.rig.Floors(t.Context(), id)
	if err != nil || len(floors) != 2 || floors[s.Kept.Handle] == "" || floors[s.Reissued.Handle] == "" {
		t.Errorf("Floors = %q, %v", floors, err)
	}
}

// Change reissues one member and resets the human's password again: the credentials of before
// are refused, the kept member's is not, and the reissued member's floor is read with its new
// token.
func TestDrillRigChangeRevokesTheReissuedTokenAndTheOldPassword(t *testing.T) {
	w := newRigWorld(t)
	id, s := w.seeded(t)
	before, err := w.rig.Floors(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if cerr := w.rig.Change(t.Context(), id, s); cerr != nil {
		t.Fatal(cerr)
	}
	for c, want := range map[drill.Credential]bool{s.Kept: true, s.Reissued: false, s.Human: false} {
		if ok, aerr := w.rig.Accepts(t.Context(), id, c); aerr != nil || ok != want {
			t.Errorf("Accepts(%s) after the change = %v, %v; want %v", c.Handle, ok, aerr, want)
		}
	}
	after, err := w.rig.Floors(t.Context(), id)
	if err != nil || after[s.Reissued.Handle] != before[s.Reissued.Handle] {
		t.Errorf("Floors after the change = %q, %v; before %q", after, err, before)
	}
}

// A missing event is named; any other failure of the read is an error, not a missing event.
func TestDrillRigMissingEventsNamesTheEventsTheServerLacks(t *testing.T) {
	w := newRigWorld(t)
	id, s := w.seeded(t)
	missing, err := w.rig.MissingEvents(t.Context(), id, append([]string{"$gone"}, s.Events...))
	if err != nil || !slices.Equal(missing, []string{"$gone"}) {
		t.Errorf("MissingEvents = %q, %v", missing, err)
	}
}

// Backup takes backup N under the scratch team's lock and answers its number.
func TestDrillRigBackupTakesTheBackupUnderTheTeamLock(t *testing.T) {
	w := newRigWorld(t)
	id := scratch(t)
	n, err := w.rig.Backup(t.Context(), id)
	if err != nil || n != 1 || !slices.Equal(w.held, []string{id.String()}) {
		t.Fatalf("Backup = %d, %v (held %q)", n, err, w.held)
	}
	w.err["backup"] = errors.New("the backup failed")
	if _, err := w.rig.Backup(t.Context(), id); err == nil {
		t.Error("a failed backup was not reported")
	}
}

// Restore runs the engine under the scratch team's lock with the drill's hooks as its
// OnStaged and AfterSwap, and a restore without hooks sets neither.
func TestDrillRigRestorePassesTheHooksToTheEngine(t *testing.T) {
	w := newRigWorld(t)
	id := scratch(t)
	var ran []string
	hooks := drill.Hooks{
		OnStaged:  func(context.Context) error { ran = append(ran, "onstaged"); return nil },
		AfterSwap: func(context.Context) error { ran = append(ran, "afterswap"); return errors.New("forced") },
	}
	err := w.rig.Restore(t.Context(), id, 3, hooks)
	if err == nil || err.Error() != "forced" || !slices.Equal(ran, []string{"onstaged", "afterswap"}) {
		t.Fatalf("Restore = %v, hooks ran %q", err, ran)
	}
	if rerr := w.rig.Restore(t.Context(), id, 3, drill.Hooks{}); rerr != nil {
		t.Fatal(rerr)
	}
	if merr := os.MkdirAll(w.root+paths.AdminState(id), 0o700); merr != nil {
		t.Fatal(merr)
	}
	if werr := os.WriteFile(w.root+paths.RestoreRecord(id), []byte("{}"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	if derr := w.rig.Discard(t.Context(), id); derr != nil {
		t.Fatal(derr)
	}
	want := []string{"restore " + id.String(), "restore " + id.String(), "discard " + id.String()}
	if !slices.Equal(w.calls, want) || !slices.Equal(w.held, []string{id.String(), id.String()}) {
		t.Errorf("calls %q, held %q", w.calls, w.held)
	}
}

// Lock is one drill-wide lock: a second drill cannot take it while the first holds it.
func TestDrillRigLockIsHeldOnce(t *testing.T) {
	w := newRigWorld(t)
	release, err := w.rig.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, busy := w.rig.Lock(t.Context()); !errors.Is(busy, teamlock.ErrBusy) {
		t.Errorf("a second Lock = %v, want busy", busy)
	}
	if rerr := release(); rerr != nil {
		t.Fatal(rerr)
	}
	again, err := w.rig.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rerr := again(); rerr != nil {
		t.Fatal(rerr)
	}
}

// A token the server refuses is not accepted, and an unreachable server is an error.
func TestDrillRigAcceptsTellsARefusalFromAFailure(t *testing.T) {
	w := newRigWorld(t)
	id, s := w.seeded(t)
	foreign := s.Kept
	foreign.Secret += "x"
	if ok, err := w.rig.Accepts(t.Context(), id, foreign); err != nil || ok {
		t.Errorf("Accepts(a foreign token) = %v, %v", ok, err)
	}
	w.rig.Base = func(int) string { return "http://127.0.0.1:1" }
	if _, err := w.rig.Accepts(t.Context(), id, s.Kept); err == nil || errors.Is(err, matrix.ErrAuthRefused) {
		t.Errorf("Accepts on an unreachable server = %v, want a failure", err)
	}
}
