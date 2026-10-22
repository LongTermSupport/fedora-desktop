package wire

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// busConn is systemd on a fake bus: every unit loaded and active in one invocation after two
// restarts; KillUnit is recorded and StopUnit refused.
type busConn struct{ calls []string }

const invocation = "abababababababababababababababab"

func (b *busConn) Call(_ context.Context, _ string, _ dbus.ObjectPath, method string, args ...any) ([]any, error) {
	b.calls = append(b.calls, method)
	switch method {
	case "org.freedesktop.DBus.GetNameOwner":
		return []any{":1.1"}, nil
	case "org.freedesktop.systemd1.Manager.LoadUnit":
		return []any{dbus.ObjectPath("/org/freedesktop/systemd1/unit/x")}, nil
	case "org.freedesktop.systemd1.Manager.StopUnit":
		return nil, errors.New("stop refused")
	case "org.freedesktop.DBus.Properties.Get":
		values := map[string]any{
			"LoadState": "loaded", "ActiveState": "active", "InvocationID": bytes.Repeat([]byte{0xab}, 16),
			"NRestarts": uint32(2), "Result": "success",
		}
		name, ok := args[1].(string)
		if !ok {
			return nil, errors.New("a property name that is not a string")
		}
		return []any{dbus.MakeVariant(values[name])}, nil
	}
	return nil, nil
}

func (*busConn) Signal(chan<- *dbus.Signal) {}

func (*busConn) Close() error { return nil }

func alphaID(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func heldLock(t *testing.T, team paths.TeamID) *teamlock.Lock {
	t.Helper()
	lock, err := teamlock.Take(t.Context(), t.TempDir(), team, "backup", 0, systemClock{}, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	return lock
}

// RW1, SM1, SM2 (BT7): the backups are backup.Take over the host: the units Manager's state and
// KillUnit, the readiness probe, the clock, and the teamchat-ctl uid read when a verb asks.
func TestBackupsAreTheEngine(t *testing.T) {
	bus := &busConn{}
	f := &fakeMachine{root: t.TempDir(), conn: bus}
	sys := build(t, f)
	d, err := sys.Backup()
	if err != nil || d.Root != f.root || d.CtlUID != 990 || d.StateOwnerUID != 0 || d.Ready != (hostnet.Readiness{}) || d.Clock != (systemClock{}) {
		t.Fatalf("Backup = %+v, %v", d, err)
	}
	team := alphaID(t)
	unit := paths.ServerUnit(team)
	st, err := d.Units.State(t.Context(), unit)
	if err != nil || st != (backup.UnitState{ActiveState: "active", InvocationID: invocation, NRestarts: 2, Result: "success"}) {
		t.Fatalf("State = %+v, %v", st, err)
	}
	if err := d.Units.Kill(t.Context(), unit, "main", 12); err != nil || bus.calls[len(bus.calls)-1] != "org.freedesktop.systemd1.Manager.KillUnit" {
		t.Fatalf("Kill = %v after %v", err, bus.calls)
	}
	var be *backup.Error
	if err := sys.Host.Backup(t.Context(), heldLock(t, team)); !errors.As(err, &be) || be.Step != backup.StepSources {
		t.Fatalf("host.Deps.Backup of an unapplied team = %v, want its sources step", err)
	}
}

// RW1, n2f-W1: with no teamchat-ctl account the backups are not built, and step 6 says why.
func TestBackupsNeedTheCtlAccount(t *testing.T) {
	team := alphaID(t)
	sys := build(t, &fakeMachine{root: t.TempDir(), noCtl: true})
	if _, err := sys.Backup(); err == nil || !strings.Contains(err.Error(), "no "+paths.CtlUser+" account") {
		t.Fatalf("Backup with no account = %v", err)
	}
	if err := sys.Host.Backup(t.Context(), heldLock(t, team)); err == nil || !strings.Contains(err.Error(), "no "+paths.CtlUser+" account") {
		t.Fatalf("host.Deps.Backup with no account = %v", err)
	}
}

// The trust rule: the teamchat-ctl account is never root, and its uid fits a uid.
func TestCtlUID(t *testing.T) {
	for _, uid := range []int{0, -1, 1 << 32} {
		if _, err := ctlUID(admin.User{UID: uid}); err == nil {
			t.Errorf("ctlUID(%d) passed", uid)
		}
	}
	if uid, err := ctlUID(admin.User{UID: 990}); err != nil || uid != 990 {
		t.Fatalf("ctlUID(990) = %d, %v", uid, err)
	}
}

const alphaTeamJSON = `{"v":1,"team":"alpha","name":"Alpha","server_name":"alpha.teamchat.internal","port":8448,` +
	`"description":"The team that builds the thing.","listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],` +
	`"url":"http://192.0.2.10:8448","humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}}`

// writeTar puts backup n of team under root as `backup` writes it.
func writeTar(t *testing.T, root string, team paths.TeamID, n int64) backup.Sources {
	t.Helper()
	src := backup.Sources{
		TeamJSON: []byte(alphaTeamJSON), Config: []byte("server_name = \"alpha.teamchat.internal\"\n"),
		Roster: apply.NewRosterFile(team).Encode(),
		Rooms:  apply.EncodeRooms(team, proto.RoomIDs{Main: "!" + strings.Repeat("a", 43), Reports: "!" + strings.Repeat("b", 43)}),
		Token:  []byte("syt_YWRtaW4_abcdefghijklmnop_0123"),
	}
	raw, err := backup.Encode(backup.Manifest{Team: team, Backup: n, TimeMS: 1792663930000, ServerName: "alpha.teamchat.internal"}, src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+paths.BackupDir(team), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+paths.BackupTar(team, n), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

// engineOf is the restore engine the wiring builds once restore is proven.
func engineOf(t *testing.T, sys *admin.System) admin.Engine {
	t.Helper()
	r, err := sys.Restore()
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	e, ok := r.(admin.Engine)
	if !ok {
		t.Fatalf("Restore = %T", r)
	}
	return e
}

// RW1, SM4, n2g-D8: once restore is proven, System.Restore is the engine over the host's own
// adapters; production stays refused (TestRestoreIsUnproven).
func TestRestoreEngineIsTheHostsAdapters(t *testing.T) {
	f := &fakeMachine{root: t.TempDir(), proven: true}
	sys := build(t, f)
	d := engineOf(t, sys).Deps
	if d.Root != f.root || d.CtlUID != 990 || d.StateOwnerUID != 0 || d.Self != "/usr/libexec/teamchat-admin" || d.FS != (restore.OS{Root: f.root}) {
		t.Fatalf("Deps %+v", d)
	}
	if d.Ready != (hostnet.Readiness{}) || d.Clock != (systemClock{}) || d.Rand != rand.Reader {
		t.Fatalf("Deps %+v", d)
	}
	if missing := nilFields(reflect.ValueOf(d)); len(missing) > 0 {
		t.Fatalf("restore.Deps seams left nil: %v", missing)
	}
	if reflect.TypeOf(d.Approvals) != reflect.TypeOf(sys.Host.Approvals) || reflect.TypeOf(d.Status) != reflect.TypeOf(sys.Host.Status) {
		t.Fatalf("Approvals %T, Status %T: not the host's", d.Approvals, d.Status)
	}
}

// RW1 (A4's glue): the restore's child is the teamchat-ctl child for the live server, and the
// secure unit's systemd-run for the staged one; the sandbox is the base unit's for the team.
func TestRestoreEngineAsksTheChild(t *testing.T) {
	d := engineOf(t, build(t, &fakeMachine{root: t.TempDir(), proven: true})).Deps
	child, ok := d.Child.(admin.RestoreChild)
	if live, isExec := child.Live.(admin.ExecChild); !ok || !isExec || live.Self != "/usr/libexec/teamchat-admin" {
		t.Fatalf("Child = %#v", d.Child)
	}
	argv := []string{"/usr/bin/systemd-run", "--wait"}
	if got := child.Unit(argv); !reflect.DeepEqual(got, admin.UnitChild{Runner: d.Run, Argv: argv}) {
		t.Fatalf("Unit = %#v", got)
	}
	team := alphaID(t)
	if got := d.Sandbox(team); !reflect.DeepEqual(got, host.TeamSandbox(team)) {
		t.Fatalf("Sandbox = %v", got)
	}
}

// SM4: the engine's units, status record, version, serve and backup reach the host's own.
func TestRestoreEngineSeamsReachTheHost(t *testing.T) {
	d := engineOf(t, build(t, &fakeMachine{root: t.TempDir(), proven: true, conn: &busConn{}})).Deps
	team := alphaID(t)
	st, err := d.Units.State(t.Context(), paths.ServerUnit(team))
	if err != nil || st != (restore.UnitState{Load: "loaded", Active: "active", InvocationID: invocation, NRestarts: 2, Result: "success"}) {
		t.Fatalf("State = %+v, %v", st, err)
	}
	if err := d.Units.Stop(t.Context(), paths.ServerUnit(team)); err == nil || !strings.Contains(err.Error(), "stop refused") {
		t.Fatalf("Stop = %v", err)
	}
	if rec, err := d.StatusRecord(team); rec != nil || err != nil {
		t.Fatalf("StatusRecord with none = %+v, %v", rec, err)
	}
	if v, err := d.Version(team); err == nil || !strings.Contains(err.Error(), "no units.json") {
		t.Fatalf("Version of an unapplied team = %v, %v", v, err)
	}
	var he *host.Error
	if err := d.Serve(t.Context(), team); !errors.As(err, &he) || he.Verb != "serve" {
		t.Fatalf("Serve of an unapplied team = %v", err)
	}
	var be *backup.Error
	if n, err := d.Backup(t.Context(), heldLock(t, team), nil); !errors.As(err, &be) || be.Step != backup.StepSources {
		t.Fatalf("Backup of an unapplied team = %d, %v", n, err)
	}
}

// SM4's RegKey and Tar: the config's registration key, none being an error, and backup N's
// tar as BK3 decodes it, a missing one an error.
func TestRestoreEngineReadsTheTarAndTheKey(t *testing.T) {
	f := &fakeMachine{root: t.TempDir(), proven: true}
	d := engineOf(t, build(t, f)).Deps
	if k, err := d.RegKey([]byte("server_name = \"x\"\n")); err == nil {
		t.Fatalf("a config with no registration key gave %q", k)
	}
	key := strings.Repeat("0f", 64)
	if got, err := d.RegKey([]byte("registration_shared_secret = \"" + key + "\"\n")); err != nil || got != key {
		t.Fatalf("RegKey = %q, %v", got, err)
	}
	team := alphaID(t)
	if tar, err := d.Tar(team, 3); err == nil || !strings.Contains(err.Error(), "there is no tar of backup 3") {
		t.Fatalf("Tar with none = %+v, %v", tar, err)
	}
	src := writeTar(t, f.root, team, 3)
	tar, err := d.Tar(team, 3)
	if err != nil {
		t.Fatal(err)
	}
	if tar.TimeMS != 1792663930000 || tar.ServerName != "alpha.teamchat.internal" || !bytes.Equal(tar.TeamFile, src.TeamJSON) ||
		!bytes.Equal(tar.Config, src.Config) || tar.Token != string(src.Token) || tar.Rooms.Main != "!"+strings.Repeat("a", 43) ||
		!bytes.Equal(tar.Roster.Encode(), src.Roster) {
		t.Fatalf("Tar = %+v", tar)
	}
}

// RW1: the engine runs over the host: with nothing recorded, a discard is refused with no
// answer; with no teamchat-ctl account the engine is not built.
func TestRestoreEngineRuns(t *testing.T) {
	e := engineOf(t, build(t, &fakeMachine{root: t.TempDir(), proven: true}))
	if out, err := e.Discard(t.Context(), nil, alphaID(t)); out != nil || admin.ExitCodeOf(err) != admin.ExitRefused {
		t.Fatalf("Discard with no record = %q, %v", out, err)
	}
	sys := build(t, &fakeMachine{root: t.TempDir(), proven: true, noCtl: true})
	if r, err := sys.Restore(); r != nil || err == nil || !strings.Contains(err.Error(), "no "+paths.CtlUser+" account") {
		t.Fatalf("Restore with no account = %v, %v", r, err)
	}
}
