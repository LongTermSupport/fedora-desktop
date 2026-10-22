package wire

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
	noCtl                      bool
	n                          counts
}

func (f *fakeMachine) machine() machine {
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
		readPin:    func() (host.Pin, error) { f.n.pins++; return host.Pin{MemoryHigh: "512M"}, f.pinErr },
		runner:     &execx.Fake{},
		dial: func(context.Context) (units.Conn, error) {
			f.n.dials++
			return nil, errors.New("no bus here")
		},
	}
}

func build(t *testing.T, f *fakeMachine) *admin.System {
	t.Helper()
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
	if err := sys.Host.Status.Refresh(t.Context(), team, false); err == nil || !strings.Contains(err.Error(), "no owner-group file") {
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
	if err := sys.Host.Status.Refresh(t.Context(), team, true); err == nil || !strings.Contains(err.Error(), "alpha has no stamp") {
		t.Fatalf("Refresh of an unstamped team = %v", err)
	}
	if line, err := sys.Host.Status.Remove(t.Context(), team); err != nil || line != "" {
		t.Fatalf("Remove with no record = %q, %v", line, err)
	}
}

// WR2, n2f-D6: a backup is refused until C 3.12 is built.
func TestBackupIsRefused(t *testing.T) {
	sys := build(t, &fakeMachine{root: t.TempDir()})
	if err := sys.Host.Backup(t.Context(), nil); err == nil || err.Error() != "backups are not built yet (C 3.12)" {
		t.Fatalf("Backup = %v", err)
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
