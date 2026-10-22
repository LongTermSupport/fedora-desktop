package wire

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

const accounts = "root:x:0:0:root:/root:/bin/bash\nu:x:1000:1000::/home/u:/bin/bash\n"

// lxcHost is a scratch host root with a passwd and a host kit whose current link names digest,
// owned by the test's uid, which the machine is told is the kit's owner.
func lxcHost(t *testing.T) *fakeMachine {
	t.Helper()
	f := &fakeMachine{root: t.TempDir(), kitOwner: os.Getuid()}
	write(t, filepath.Join(f.root, "etc/passwd"), accounts)
	kits := filepath.Join(f.root, paths.HostKitRoot)
	if err := os.MkdirAll(filepath.Join(kits, digest), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(digest, filepath.Join(kits, "current")); err != nil {
		t.Fatal(err)
	}
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteFile(path, []byte(content), 0o644, -1); err != nil {
		t.Fatal(err)
	}
}

func team(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The LXC seams are built at the first lxc verb, once: the host's paths, the passwd's uids, the
// current kit, the owner group; only the Roster and Refresh are left to the verbs.
func TestLXCDepsAreTheHosts(t *testing.T) {
	f := lxcHost(t)
	sys := build(t, f)
	if f.n != (counts{}) {
		t.Fatalf("building the System read the host: %+v", f.n)
	}
	d, err := sys.LXC()
	if err != nil {
		t.Fatalf("LXC: %v", err)
	}
	if missing := nilFields(reflect.ValueOf(*d), "Roster", "Refresh"); len(missing) > 0 {
		t.Errorf("lxc.Deps seams left nil: %v", missing)
	}
	want := paths.HostKitDir(mustDigest(t)).String()
	if d.Root != f.root || d.LockDir != f.root+paths.AdminRunDir || d.Kit.String() != digest || d.HostKit != f.root+want ||
		!reflect.DeepEqual(d.HostUIDs, map[int]bool{0: true, 1000: true}) || d.OwnerGID != 1000 || d.Bridge != Bridge {
		t.Fatalf("deps %+v", d)
	}
}

// LXC is built once; its approvals are the approvals files, load only.
func TestLXCDepsAreBuiltOnce(t *testing.T) {
	f := lxcHost(t)
	sys := build(t, f)
	d, err := sys.LXC()
	if err != nil {
		t.Fatal(err)
	}
	if g, lerr := d.Approvals.Load(team(t)); lerr != nil || g != nil {
		t.Fatalf("an approval nobody gave = %+v, %v", g, lerr)
	}
	again, err := sys.LXC()
	if err != nil || again != d || f.n.groups != 1 {
		t.Fatalf("a second call built again: %v, %d group reads", err, f.n.groups)
	}
}

func mustDigest(t *testing.T) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// badHost is a host fact broken, and what the error must name.
type badHost struct {
	breakIt func(t *testing.T, f *fakeMachine)
	want    string
}

func kitRoot(f *fakeMachine) string { return filepath.Join(f.root, paths.HostKitRoot) }

func relink(t *testing.T, f *fakeMachine, target string) {
	t.Helper()
	if err := os.Remove(filepath.Join(kitRoot(f), "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(kitRoot(f), "current")); err != nil {
		t.Fatal(err)
	}
}

func badHosts() map[string]badHost {
	return map[string]badHost{
		"no passwd": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			if err := os.Remove(filepath.Join(f.root, "etc/passwd")); err != nil {
				t.Fatal(err)
			}
		}, "no /etc/passwd"},
		"a passwd that is a directory": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			file := filepath.Join(f.root, "etc/passwd")
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0o750); err != nil {
				t.Fatal(err)
			}
		}, "the host's /etc/passwd"},
		"a passwd that does not parse": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			write(t, filepath.Join(f.root, "etc/passwd"), "not a passwd line\n")
		}, "passwd"},
		"no kit root": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			if err := os.RemoveAll(kitRoot(f)); err != nil {
				t.Fatal(err)
			}
		}, "the host kit"},
		"no current link": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			if err := os.Remove(filepath.Join(kitRoot(f), "current")); err != nil {
				t.Fatal(err)
			}
		}, "current"},
		"current is a directory": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			if err := os.Remove(filepath.Join(kitRoot(f), "current")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(kitRoot(f), "current"), 0o750); err != nil {
				t.Fatal(err)
			}
		}, "not a link"},
		"current names no digest": {func(t *testing.T, f *fakeMachine) { t.Helper(); relink(t, f, "../elsewhere") }, "digest"},
		"another owner":           {func(_ *testing.T, f *fakeMachine) { f.kitOwner++ }, "owned by uid"},
		"a kit root others write": {func(t *testing.T, f *fakeMachine) {
			t.Helper()
			if err := unix.Chmod(kitRoot(f), 0o770); err != nil {
				t.Fatal(err)
			}
		}, "group or others"},
		"no owner group": {func(_ *testing.T, f *fakeMachine) { f.groupErr = errors.New("no owner-group file") }, "no owner-group file"},
	}
}

// n3-D26, n3-D29: each host fact the seams need fails LXC, naming it.
func TestLXCDepsRefuseABadHost(t *testing.T) {
	for name, c := range badHosts() {
		t.Run(name, func(t *testing.T) {
			f := lxcHost(t)
			c.breakIt(t, f)
			if _, err := build(t, f).LXC(); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("LXC = %v, want %q", err, c.want)
			}
		})
	}
}

// Origin is the stamp's: no stamp is an error, a stamped team its origin.
func TestLXCOriginIsTheStamps(t *testing.T) {
	f := lxcHost(t)
	d, err := build(t, f).LXC()
	if err != nil {
		t.Fatal(err)
	}
	if _, oerr := d.Origin(t.Context(), team(t)); oerr == nil || !strings.Contains(oerr.Error(), "alpha is not stamped") {
		t.Fatalf("unstamped: %v", oerr)
	}
	doc := `{"team":{"team":"alpha","state":"present","name":"alpha","port":8448,"description":"d",` +
		`"listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],"humans":["alice"],` +
		`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},` +
		`"lxc_members":[],"address":"192.0.2.10"}`
	in, err := declare.DecodeInput([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.root, paths.TeamDeclaration(team(t))), string(in.Team.Canonical()))
	write(t, filepath.Join(f.root, paths.StampFile(team(t))),
		`{"v":1,"team":"alpha","config":"`+strings.Repeat("a", 64)+`","software":"abc","tuwunel":"1.9.4","applied_ms":42}`)
	if origin, oerr := d.Origin(t.Context(), team(t)); oerr != nil || origin != "v2" {
		t.Fatalf("stamped: %q, %v", origin, oerr)
	}
	write(t, filepath.Join(f.root, paths.StampFile(team(t))), `{`)
	if _, oerr := d.Origin(t.Context(), team(t)); oerr == nil {
		t.Fatal("a stamp that does not decode")
	}
}

// V1 is not built until N6: asked, it fails naming N6.
func TestLXCV1HoldbackIsNotBuilt(t *testing.T) {
	d, err := build(t, lxcHost(t)).LXC()
	if err != nil {
		t.Fatal(err)
	}
	if _, verr := d.V1.Holds(t.Context(), nil, paths.GuestUser{}, team(t)); verr == nil || !strings.Contains(verr.Error(), "N6") {
		t.Fatalf("Holds = %v", verr)
	}
}

// dialRoot is a guest root whose systemd socket is one end of a socketpair, the other closed,
// or no socket at all.
type dialRoot struct {
	lxc.Root

	err error
}

func (r dialRoot) DialSocket(_ context.Context, p string) (*net.UnixConn, error) {
	if p != guestSystemdSocket {
		return nil, errors.New("the bus dialled " + p)
	}
	if r.err != nil {
		return nil, r.err
	}
	return hungUp()
}

// hungUp is one end of a socketpair whose other end is closed.
func hungUp() (*net.UnixConn, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if cerr := unix.Close(fds[1]); cerr != nil {
		return nil, errors.Join(cerr, unix.Close(fds[0]))
	}
	f := os.NewFile(uintptr(fds[0]), "bus")
	c, err := net.FileConn(f)
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return nil, errors.Join(errors.New("not a unix socket"), c.Close())
	}
	return uc, nil
}

// Bus dials the guest's systemd socket through the root (TB1) and speaks D-Bus on it; a socket
// that will not open, or a peer that does not authenticate, fails.
func TestLXCBusDialsTheGuestsSystemd(t *testing.T) {
	d, err := build(t, lxcHost(t)).LXC()
	if err != nil {
		t.Fatal(err)
	}
	if _, berr := d.Bus(t.Context(), dialRoot{err: guestroot.ErrRefused}); !errors.Is(berr, guestroot.ErrRefused) {
		t.Fatalf("no socket: %v", berr)
	}
	if _, berr := d.Bus(t.Context(), dialRoot{}); berr == nil {
		t.Fatal("a peer that hung up authenticated")
	}
}

// Pin is the init pid lxc-info answers, pinned under the machine's /proc: a pid that cannot be
// had fails the pin. The health tick's Guests is lxc.Guests over the same seams.
func TestLXCPinAndGuests(t *testing.T) {
	f := lxcHost(t)
	d, err := build(t, f).LXC()
	if err != nil {
		t.Fatal(err)
	}
	if r, perr := d.Pin(t.Context(), "box-1"); perr == nil || r != nil {
		t.Fatalf("Pin with no lxc-info = %v, %v", r, perr)
	}
	g := f.machine().guests()
	if g.Root != f.root || g.LXC == nil || g.Pin == nil {
		t.Fatalf("guests %+v", g)
	}
	var _ health.Guests = g
	probe, err := g.Probe(t.Context(), team(t), "thing.1+box-1.lxc")
	if err != nil || !probe.Stopped {
		t.Fatalf("a team with no record = %+v, %v", probe, err)
	}
}
