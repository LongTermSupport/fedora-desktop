package wire

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	v1 "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/migrate/v1"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/units"
)

const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// counts is how often each host read the wiring makes was made.
type counts struct{ dials, groups, pins, users int }

// fakeMachine is a host the tests decide: a scratch root, a teamchat-ctl account, an owner
// group and a pin, each failing when its error is set.
type fakeMachine struct {
	root                       string
	selfErr, userErr, groupErr error
	pinErr                     error
	kitOwner                   int
	conn                       units.Conn
	n                          counts
	owner                      uint32 // the uid the state's root-owned files are given
	noCtl                      bool
	proven                     bool   // a proof record that enables restore is written under root
	tuwunel                    string // the pin's Tuwunel version; "" is pinnedTuwunel
	systemd                    string // the Manager's Version property; "" is pinnedSystemd
}

// pinnedSystemd is the fake bus's systemd version unless a test names another.
const pinnedSystemd = "256.7-1.fc41"

// versionConn is the bus a fakeMachine has when a test gives it none: systemd answers its
// Version property and nothing else, so a unit question fails as "no bus here".
type versionConn struct {
	version string
	mu      sync.Mutex
	signals chan<- *dbus.Signal
}

func (v *versionConn) Call(_ context.Context, _ string, _ dbus.ObjectPath, method string, args ...any) ([]any, error) {
	switch method {
	case "org.freedesktop.DBus.AddMatch", "org.freedesktop.systemd1.Manager.Subscribe":
		return nil, nil
	case "org.freedesktop.DBus.GetNameOwner":
		return []any{":1.1"}, nil
	case "org.freedesktop.DBus.Properties.Get":
		if len(args) == 2 && args[0] == "org.freedesktop.systemd1.Manager" && args[1] == "Version" {
			return []any{dbus.MakeVariant(v.version)}, nil
		}
	}
	return nil, errors.New("no bus here")
}

func (v *versionConn) Signal(ch chan<- *dbus.Signal) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.signals = ch
}

func (v *versionConn) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.signals != nil {
		close(v.signals)
		v.signals = nil
	}
	return nil
}

func (f *fakeMachine) machine() machine {
	systemd := f.systemd
	if systemd == "" {
		systemd = pinnedSystemd
	}
	return machine{
		root: f.root, proc: f.root + "/proc",
		self: func() (string, error) { return "/usr/libexec/teamchat-admin", f.selfErr },
		lookupUser: func(name string) (admin.User, bool, error) {
			f.n.users++
			if name != paths.CtlUser || f.noCtl {
				return admin.User{}, false, f.userErr
			}
			return admin.User{UID: 990, GID: 985}, true, f.userErr
		},
		ownerGroup: func() (int, error) { f.n.groups++; return 1000, f.groupErr },
		readPin: func() (host.Pin, error) {
			f.n.pins++
			return host.Pin{MemoryHigh: "512M", Version: pinVersion(f.tuwunel)}, f.pinErr
		},
		runner: &execx.Fake{}, kernel: guestroot.LinuxKernel{}, kitOwner: f.kitOwner,
		dial: func(context.Context) (units.Conn, error) {
			f.n.dials++
			if f.conn != nil {
				return f.conn, nil
			}
			return &versionConn{version: systemd}, nil
		},
		stateOwnerUID: f.owner,
	}
}

// ownerOf is the uid that owns dir: a fixture is its test user's, where root's own files on a
// host are root's (production).
func ownerOf(t *testing.T, dir string) uint32 {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no owner in a stat")
	}
	return st.Uid
}

// pinnedTuwunel is the fake pin's Tuwunel version unless a test names another.
const pinnedTuwunel = "v1.2.3"

func pinVersion(s string) paths.ServerVersion {
	if s == "" {
		s = pinnedTuwunel
	}
	v, err := paths.ParseServerVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// proofOf is a record of every clause passed on this test build and the pinned Tuwunel.
func proofOf(t *testing.T) restore.Proof {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[restore.Clause]restore.Verdict{}
	for _, c := range restore.Clauses {
		checks[c] = restore.VerdictPass
	}
	return restore.Proof{Key: restore.Key{Software: d, Tuwunel: pinVersion(""), Systemd: pinnedSystemd}, Checks: checks, PassedMS: 1, Where: restore.WhereVMLab}
}

// writeProof puts p under the machine's root, owned by the uid the wiring trusts there.
func writeProof(t *testing.T, f *fakeMachine, p restore.Proof) {
	t.Helper()
	if err := os.MkdirAll(f.root+paths.AdminStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	f.owner = ownerOf(t, f.root)
	if err := restore.WriteProof(f.root, p); err != nil {
		t.Fatal(err)
	}
}

func build(t *testing.T, f *fakeMachine) *admin.System {
	t.Helper()
	if f.proven {
		writeProof(t, f, proofOf(t))
	}
	sys, err := f.machine().system(digest)
	if err != nil {
		t.Fatalf("system: %v", err)
	}
	return sys
}

// nilFields names the func and interface fields of v left nil, but those named in skip.
func nilFields(v reflect.Value, skip ...string) []string {
	var out []string
	for i := range v.NumField() {
		f, name := v.Field(i), v.Type().Field(i).Name
		k := f.Kind()
		if (k == reflect.Func || k == reflect.Interface || k == reflect.Pointer) && f.IsNil() && !slices.Contains(skip, name) {
			out = append(out, name)
		}
	}
	return out
}

// RW2, n2g-D8: production refuses the restore engine with no proof record, and with one that is
// not of this build, not of the pinned Tuwunel or has a clause not passed; each names it (70).
func TestRestoreIsUnproven(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir()})
	if engine, err := sys.Restore(t.Context()); engine != nil || !errors.Is(err, restore.ErrUnproven) || admin.ExitCodeOf(err) != admin.ExitRefused ||
		!strings.Contains(err.Error(), "no restore drill has passed") {
		t.Fatalf("Restore = %v, %v", engine, err)
	}
	for name, tc := range map[string]struct {
		edit    func(*restore.Proof)
		tuwunel string
		systemd string
		want    string
	}{
		"another tuwunel":  {func(*restore.Proof) {}, "v2.0.0", "", "for Tuwunel v1.2.3, not v2.0.0"},
		"another systemd":  {func(*restore.Proof) {}, "", "257.1-1.fc42", "for systemd 256.7-1.fc41, not 257.1-1.fc42"},
		"a skipped clause": {func(p *restore.Proof) { p.Checks[restore.ClauseHeldSession] = restore.VerdictSkipped }, "", "", "not passed: H-C15.held-session"},
		"another build": {func(p *restore.Proof) {
			d, err := paths.ParseDigest(strings.Repeat("f", 64))
			if err != nil {
				t.Fatal(err)
			}
			p.Software = d
		}, "", "", "for another build"},
	} {
		f := &fakeMachine{root: t.TempDir(), tuwunel: tc.tuwunel, systemd: tc.systemd}
		p := proofOf(t)
		tc.edit(&p)
		writeProof(t, f, p)
		if engine, err := build(t, f).Restore(t.Context()); engine != nil || !errors.Is(err, restore.ErrUnproven) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Restore = %v, %v", name, engine, err)
		}
	}
}

// The drill is the host's, and needs no proof record (it makes one): drill.Run over the rig,
// the TCP dial hostnet.Answers, the restore engine's Deps without the proof gate and with its
// hooks unset, and the proof's key read as Restore reads it.
func TestTheDrillIsTheHostsAndNeedsNoProof(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir()})
	d, err := sys.Drill(t.Context())
	if err != nil {
		t.Fatalf("Drill: %v", err)
	}
	hd, ok := d.(admin.HostDrill)
	if !ok || hd.Sys != sys {
		t.Fatalf("Drill = %#v", d)
	}
	if missing := nilFields(reflect.ValueOf(hd)); len(missing) != 0 {
		t.Fatalf("the drill leaves %q unset", missing)
	}
	if reflect.ValueOf(hd.Drill).Pointer() != reflect.ValueOf(drill.Run).Pointer() ||
		reflect.ValueOf(hd.Dial).Pointer() != reflect.ValueOf(hostnet.Answers).Pointer() {
		t.Errorf("the drill is not drill.Run, or its dial not hostnet.Answers")
	}
	drillSeams(t, hd)
}

// drillSeams holds the drill's Deps (hooks unset), its key (Restore's) and its clean-up engine.
func drillSeams(t *testing.T, hd admin.HostDrill) {
	t.Helper()
	deps, err := hd.Deps(t.Context())
	if err != nil || deps.OnStaged != nil || deps.AfterSwap != nil || deps.Child == nil || deps.FS == nil {
		t.Fatalf("Deps = %+v, %v", deps, err)
	}
	key, err := hd.Key(t.Context())
	if want := proofOf(t).Key; err != nil || key != want {
		t.Fatalf("Key = %+v, %v, want %+v", key, err, want)
	}
	engine, err := hd.Engine(t.Context())
	if err != nil || engine == nil {
		t.Fatalf("Engine = %v, %v", engine, err)
	}
}

// The proof's key is read when the seam is called: a pin or a systemd version that cannot be read
// fails the seam.
func TestRestoreNeedsThePinAndTheSystemdVersion(t *testing.T) {
	f := &fakeMachine{root: t.TempDir(), pinErr: errors.New("no pin")}
	if engine, err := build(t, f).Restore(t.Context()); engine != nil || err == nil || err.Error() != "no pin" {
		t.Fatalf("Restore with no pin = %v, %v", engine, err)
	}
	f = &fakeMachine{root: t.TempDir(), proven: true, systemd: "not systemd"}
	if engine, err := build(t, f).Restore(t.Context()); engine != nil || err == nil || !strings.Contains(err.Error(), "Version is not a systemd version") {
		t.Fatalf("Restore with no systemd version = %v, %v", engine, err)
	}
}

// Plan 00178: the v1 history export runs over the host's root, root as the owner of root's files,
// the account database and the loopback client; building it reads nothing.
func TestHistoryExportIsTheHosts(t *testing.T) {
	f := &fakeMachine{root: t.TempDir(), owner: 7}
	sys := build(t, f)
	seam, err := sys.HistoryExport()
	if err != nil {
		t.Fatal(err)
	}
	x, ok := seam.(v1.Exporter)
	if !ok || x.Deps.Root != f.root || x.Deps.RootUID != 7 || x.Deps.HTTP == nil || x.Deps.Now == nil || x.Deps.Chown == nil || f.n != (counts{}) {
		t.Fatalf("%T %+v %+v", seam, x.Deps, f.n)
	}
	acct, found, err := x.Deps.Lookup(paths.CtlUser)
	if err != nil || !found || acct != (v1.Account{UID: 990, GID: 985}) || f.n.users != 1 {
		t.Fatalf("%+v %v %v", acct, found, err)
	}
}

// WR2: every seam is set, and building the System reads nothing: no bus, no account, no group,
// no pin.
func TestEverySeamIsSetAndNothingIsRead(t *testing.T) {
	f := &fakeMachine{root: t.TempDir()}
	sys := build(t, f)
	if missing := nilFields(reflect.ValueOf(*sys)); len(missing) > 0 {
		t.Errorf("System seams left nil: %v", missing)
	}
	if missing := nilFields(reflect.ValueOf(sys.Host), "Bootstrap"); len(missing) > 0 {
		t.Errorf("host.Deps seams left nil: %v", missing)
	}
	if f.n != (counts{}) {
		t.Fatalf("building the System read the host: %+v", f.n)
	}
	if sys.Host.Root != f.root || sys.Host.LockDir != f.root+paths.AdminRunDir || sys.Host.Software != digest {
		t.Fatalf("host %+v", sys.Host)
	}
	ids, err := sys.CtlIDs()
	if err != nil || ids != (admin.User{UID: 990, GID: 985}) {
		t.Fatalf("CtlIDs = %+v, %v", ids, err)
	}
	if pin, err := sys.Pin(); err != nil || pin.MemoryHigh != "512M" {
		t.Fatalf("Pin = %+v, %v", pin, err)
	}
	if _, err := sys.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if gid, err := sys.OwnerGroup(); err != nil || gid != 1000 || f.n.groups != 1 {
		t.Fatalf("OwnerGroup = %d, %v after %d reads (the tick and the verb share one)", gid, err, f.n.groups)
	}
}

// WR2, n2f-W1: a part that cannot be built fails only the seam that needs it, naming it.
func TestAPartThatCannotBeBuiltFailsItsSeam(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir(), noCtl: true})
	if _, err := sys.CtlIDs(); err == nil || !strings.Contains(err.Error(), "no "+paths.CtlUser+" account") {
		t.Fatalf("CtlIDs with no account = %v", err)
	}
	sys = build(t, &fakeMachine{root: t.TempDir(), userErr: errors.New("nss down")})
	if _, err := sys.CtlIDs(); err == nil || !strings.Contains(err.Error(), "nss down") {
		t.Fatalf("CtlIDs with the lookup failing = %v", err)
	}
	sys = build(t, &fakeMachine{root: t.TempDir(), pinErr: errors.New("no pin file")})
	if _, err := sys.Pin(); err == nil || !strings.Contains(err.Error(), "no pin file") {
		t.Fatalf("Pin = %v", err)
	}
}

// n2f-W1: no owner group fails the tick and the status record, not the wiring; the running
// binary and the digest are the System's own and fail the wiring.
func TestNoOwnerGroupFailsTheTickAndTheStatus(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir(), groupErr: errors.New("no owner-group file")})
	if _, err := sys.Health(); err == nil || !strings.Contains(err.Error(), "the owner group: no owner-group file") {
		t.Fatalf("Health with no owner group = %v", err)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.Host.Status.Refresh(t.Context(), team, false, nil); err == nil || !strings.Contains(err.Error(), "no owner-group file") {
		t.Fatalf("Status.Refresh with no owner group = %v", err)
	}
	if _, err := sys.Host.Status.Remove(t.Context(), team); err == nil || !strings.Contains(err.Error(), "no owner-group file") {
		t.Fatalf("Status.Remove with no owner group = %v", err)
	}
	if _, err := (&fakeMachine{root: t.TempDir(), selfErr: errors.New("no /proc")}).machine().system(digest); err == nil || !strings.Contains(err.Error(), "the running binary: no /proc") {
		t.Fatalf("system with no running binary = %v", err)
	}
	if _, err := (&fakeMachine{root: t.TempDir()}).machine().system(""); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("system with no digest = %v", err)
	}
}

// WR2: the status record is the health tick's over the scratch root.
func TestStatusIsTheTicks(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir()})
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.Host.Status.Refresh(t.Context(), team, true, nil); err == nil || !strings.Contains(err.Error(), "alpha has no stamp") {
		t.Fatalf("Refresh of an unstamped team = %v", err)
	}
	if line, err := sys.Host.Status.Remove(t.Context(), team); err != nil || line != "" {
		t.Fatalf("Remove with no record = %q, %v", line, err)
	}
}

// caWall is a pki clock stopped at one millisecond.
type caWall struct{}

func (caWall) WallMS() int64 { return 1_790_000_000_000 }

// plantCA writes a new CA for team where the host keeps it and answers its certificate.
func plantCA(t *testing.T, root string, team paths.TeamID) []byte {
	t.Helper()
	ca, err := pki.Maker{Clock: caWall{}}.NewCA("teamchat " + team.String() + " CA")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(root+paths.CADir(team), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(root+paths.CAKeyFile(team), ca.KeyPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(root+paths.CACertFile(team), ca.CertPEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	return ca.CertPEM()
}

// Plan 00246 S16 (review blocker B2 of pull request 128): the production System's team CA, and
// the one its seat bundles carry, is the CA the host keeps for the team (host.CAPEM): an applied
// team's certificate exactly, ErrTeamCANotMade for a team with none, and a refusal naming the file
// for a CA the host could not sign a leaf with. So an https team's bundle and LXC reach fact carry
// the team's CA, and a team with none issues nothing instead of a bundle without it.
func TestTeamCAIsTheHostsCA(t *testing.T) {
	root := t.TempDir()
	sys := build(t, &fakeMachine{root: root, owner: ownerOf(t, root)})
	bundles, ok := sys.Bundles.(admin.SeatBundles)
	if !ok {
		t.Fatalf("Bundles is a %T, want the seat side's", sys.Bundles)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	providers := map[string]admin.TeamCA{"System.TeamCA": sys.TeamCA, "SeatBundles.CA": bundles.CA}
	for name, ca := range providers {
		if _, err := ca(team); !errors.Is(err, ErrTeamCANotMade) {
			t.Errorf("%s of a team with no CA = %v, want ErrTeamCANotMade", name, err)
		}
	}
	cert := plantCA(t, root, team)
	for name, ca := range providers {
		if got, err := ca(team); err != nil || got != string(cert) {
			t.Errorf("%s of an applied team = %v; want the CA certificate the host keeps", name, err)
		}
	}
	if err := os.Remove(root + paths.CAKeyFile(team)); err != nil {
		t.Fatal(err)
	}
	for name, ca := range providers {
		if _, err := ca(team); err == nil || errors.Is(err, ErrTeamCANotMade) || !strings.Contains(err.Error(), "ca.key") {
			t.Errorf("%s of a CA without its key = %v, want a refusal naming ca.key", name, err)
		}
	}
}

// X5: the production System's LXC seats' url, and the one its seat bundles give an LXC seat, is
// read from the host's applied files: a team never applied has none, never the team's url.
func TestLXCURLIsTheHostsAppliedOne(t *testing.T) {
	root := t.TempDir()
	sys := build(t, &fakeMachine{root: root, owner: ownerOf(t, root)})
	bundles, ok := sys.Bundles.(admin.SeatBundles)
	if !ok {
		t.Fatalf("Bundles is a %T, want the seat side's", sys.Bundles)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	decl := declare.Team{ID: team, URL: "https://chat.example.com:8448", Port: 8448}
	for name, provider := range map[string]func(declare.Team) (string, error){"System.LXCURL": sys.LXCURL, "SeatBundles.LXCURL": bundles.LXCURL} {
		if provider == nil {
			t.Errorf("%s is not wired", name)
			continue
		}
		if got, err := provider(decl); err == nil {
			t.Errorf("%s of a team never applied = %q", name, got)
		}
	}
}

// WR2: New is the host's wiring; building it reads only the running binary.
func TestNewIsTheHostsWiring(t *testing.T) {
	sys, err := New(digest)()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sys.Host.Root != "" || sys.Host.LockDir != paths.AdminRunDir {
		t.Fatalf("host paths %+v", sys.Host)
	}
	if sys.Host.StateOwnerUID != 0 {
		t.Fatalf("the host's state is owned by uid %d, want root's", sys.Host.StateOwnerUID)
	}
	if _, err := New("not a digest")(); err == nil {
		t.Fatal("New without a digest wired")
	}
}

func TestSystemClock(t *testing.T) {
	c := systemClock{}
	if c.Now().IsZero() {
		t.Fatal("Now is zero")
	}
	if err := c.Sleep(t.Context(), 0); err != nil {
		t.Fatalf("Sleep(0) = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Sleep(ctx, 1<<40); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep after cancel = %v", err)
	}
}

// RG3: the host steps and the member verbs are guarded by the restore record under the root.
func TestTheGuardIsTheRestoreRecordUnderTheRoot(t *testing.T) {
	f := &fakeMachine{root: t.TempDir()}
	sys := build(t, f)
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.Host.Guard(team); err != nil {
		t.Fatalf("Guard with no record = %v", err)
	}
	if err := os.MkdirAll(f.root+paths.AdminState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.root+paths.RestoreRecord(team), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sys.Host.Guard(team); !errors.Is(err, restore.ErrPending) {
		t.Fatalf("Guard with a record = %v, want restore.ErrPending", err)
	}
}

// carriedRoot is a host prefix where team ops has the record at path.
func carriedRoot(t *testing.T, path func(paths.TeamID) string) (string, paths.TeamID) {
	t.Helper()
	root := t.TempDir()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{paths.AdminState(team), paths.CarryDir} {
		if err := os.MkdirAll(root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(root+path(team), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, team
}

// SG3, SG4: the lock's guard also refuses each carry record (seal, drill, import), and team
// remove's guard refuses the drill and the import record but admits the seal.
func TestTheGuardsRefuseTheCarryRecords(t *testing.T) {
	for name, path := range map[string]func(paths.TeamID) string{
		"seal": paths.SealRecord, "drill": paths.DrillRecord, "import": paths.CarryImportRecord,
	} {
		root, team := carriedRoot(t, path)
		sys := build(t, &fakeMachine{root: root})
		if err := sys.Host.Guard(team); !errors.Is(err, carry.ErrSealed) {
			t.Errorf("%s: Guard = %v, want carry.ErrSealed", name, err)
		}
		err := sys.Host.RemoveGuard(team)
		switch {
		case name == "seal" && err != nil:
			t.Errorf("RemoveGuard of a sealed team = %v, want nil", err)
		case name != "seal" && !errors.Is(err, carry.ErrSealed):
			t.Errorf("%s: RemoveGuard = %v, want carry.ErrSealed", name, err)
		}
	}
}

// SG2, SG3: the lock's guard, the host's and the LXC verbs', refuses a drill import (read as
// root's own file) as a refusal (70); team remove's guard admits it.
func TestTheGuardsRefuseADrillImport(t *testing.T) {
	f := lxcHost(t)
	team := team(t)
	if err := os.MkdirAll(f.root+paths.AdminState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := carry.EncodeCarriedRecord(carry.CarriedRecord{
		Team: team, Kind: carry.KindDrill, SHA256: strings.Repeat("a", 64), SealedMS: 1, ImportedMS: 2,
		Tail: []carry.TailEntry{{Room: "main", EventID: "$event-1", TsMS: 1}},
	})
	if err := os.WriteFile(f.root+paths.CarriedRecord(team), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	f.owner = ownerOf(t, f.root)
	sys := build(t, f)
	if err := sys.Host.Guard(team); !errors.Is(err, carry.ErrRefused) {
		t.Errorf("Guard = %v, want carry.ErrRefused", err)
	}
	if err := sys.Host.RemoveGuard(team); err != nil {
		t.Errorf("RemoveGuard = %v", err)
	}
	d, err := sys.LXC()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Guard(team); !errors.Is(err, carry.ErrRefused) {
		t.Errorf("lxc Guard = %v, want carry.ErrRefused", err)
	}
}

// SG3: the LXC verbs' lock guard refuses a sealed team as the host's does.
func TestLXCGuardRefusesASealedTeam(t *testing.T) {
	f := lxcHost(t)
	team := team(t)
	for _, dir := range []string{paths.AdminState(team), paths.CarryDir} {
		if err := os.MkdirAll(f.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.root+paths.SealRecord(team), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := build(t, f).LXC()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Guard(team); !errors.Is(err, carry.ErrSealed) {
		t.Fatalf("lxc Guard = %v, want carry.ErrSealed", err)
	}
}
