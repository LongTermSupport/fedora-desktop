package units

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const busConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=%s</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// scratch is a private dbus-daemon.
type scratch struct {
	address string
	pid     int
}

func (s scratch) stop() error {
	if err := unix.Kill(s.pid, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("stop dbus-daemon %d: %w", s.pid, err)
	}
	return nil
}

// scratchBus starts a private dbus-daemon from a config written here; cleanup stops it.
func scratchBus(ctx context.Context, t *testing.T) scratch {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon is not installed, so the wire test cannot run: " + err.Error())
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(config, fmt.Appendf(nil, busConfig, filepath.Join(dir, "bus")), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := execx.OS{}.Run(ctx, execx.Cmd{
		Name: "dbus-daemon", Args: []string{"--config-file=" + config, "--fork", "--print-address=1", "--print-pid=1"},
	})
	if err != nil {
		t.Fatalf("dbus-daemon: %v", err)
	}
	lines := strings.Fields(string(res.Stdout))
	if len(lines) != 2 {
		t.Fatalf("dbus-daemon printed %q", res.Stdout)
	}
	pid, err := strconv.Atoi(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	s := scratch{address: lines[0], pid: pid}
	t.Cleanup(func() {
		if err := s.stop(); err != nil {
			t.Error(err)
		}
	})
	return s
}

type unitChange struct{ Type, File, Destination string }

// fakeSystemd is org.freedesktop.systemd1's Manager as the adapter calls it, exported on a
// scratch bus. Its exported methods are D-Bus methods by name and signature.
type fakeSystemd struct {
	conn *dbus.Conn
	unit string

	mu     sync.Mutex
	result string
	after  bool
	next   uint32
	calls  []string
}

func (f *fakeSystemd) StartUnit(name, mode string) (dbus.ObjectPath, *dbus.Error) {
	return f.job("StartUnit", name, mode)
}

func (f *fakeSystemd) RestartUnit(name, mode string) (dbus.ObjectPath, *dbus.Error) {
	return f.job("RestartUnit", name, mode)
}

func (f *fakeSystemd) StopUnit(name, mode string) (dbus.ObjectPath, *dbus.Error) {
	return f.job("StopUnit", name, mode)
}

func (f *fakeSystemd) Reload() *dbus.Error {
	f.record("Reload")
	return nil
}

func (f *fakeSystemd) Subscribe() *dbus.Error {
	f.record("Subscribe")
	return nil
}

func (f *fakeSystemd) EnableUnitFiles(files []string, runtime, force bool) (bool, []unitChange, *dbus.Error) {
	f.record(fmt.Sprintf("EnableUnitFiles %v %v %v", files, runtime, force))
	return false, []unitChange{{"symlink", "/etc/systemd/system/x", files[0]}}, nil
}

func (f *fakeSystemd) DisableUnitFiles(files []string, runtime bool) ([]unitChange, *dbus.Error) {
	f.record(fmt.Sprintf("DisableUnitFiles %v %v", files, runtime))
	return []unitChange{}, nil
}

func (f *fakeSystemd) LoadUnit(name string) (dbus.ObjectPath, *dbus.Error) {
	f.record("LoadUnit " + name)
	if name != f.unit {
		return "", dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []any{"Unit " + name + " not found."})
	}
	return unitPath, nil
}

func (f *fakeSystemd) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeSystemd) job(method, name, mode string) (dbus.ObjectPath, *dbus.Error) {
	f.record(method + " " + name + " " + mode)
	if strings.Contains(name, "masked") {
		return "", dbus.NewError("org.freedesktop.systemd1.UnitMasked", []any{"Unit " + name + " is masked."})
	}
	f.mu.Lock()
	f.next++
	id, result, after := f.next, f.result, f.after
	f.mu.Unlock()
	job := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/systemd1/job/%d", id))
	emit := func() error { return f.conn.Emit(managerPath, jobRemovedSignal, id, job, name, result) }
	if after {
		go func() {
			if err := emit(); err != nil {
				panic(err)
			}
		}()
		return job, nil
	}
	if err := emit(); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return job, nil
}

func (f *fakeSystemd) recorded() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

// exportSystemd owns org.freedesktop.systemd1 on the bus at address and exports the fake.
func exportSystemd(t *testing.T, address, unit string) *fakeSystemd {
	t.Helper()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := (busConn{conn}).Close(); cerr != nil {
			t.Errorf("close the fake: %v", cerr)
		}
	})
	reply, err := conn.RequestName(destination, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", destination, reply, err)
	}
	f := &fakeSystemd{conn: conn, unit: unit, result: jobDone}
	if err := conn.Export(f, managerPath, managerIface); err != nil {
		t.Fatal(err)
	}
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitFalse} }
	if _, err := prop.Export(conn, unitPath, prop.Map{
		unitIface: {
			"ActiveState": ro("active"), "SubState": ro("running"), "UnitFileState": ro("enabled"),
			"InvocationID":         ro([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}),
			"ActiveEnterTimestamp": ro(uint64(1_000_000)),
		},
		serviceIface: {"NRestarts": ro(uint32(2))},
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSystemdOverARealBus(t *testing.T) {
	bus := scratchBus(t.Context(), t)
	unit := testUnit(t)
	f := exportSystemd(t, bus.address, unit)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", bus.address)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	lost := make(chan struct{}, 4)
	m := NewManager(SystemBus, &execx.Fake{})
	m.onSignal = func(s *dbus.Signal) {
		if s == nil {
			lost <- struct{}{}
		}
	}
	wireJobs(ctx, t, m, f, unit)
	wireStates(ctx, t, m, unit)

	want := strings.Join([]string{
		"Subscribe",
		"StartUnit " + unit + " replace",
		"RestartUnit " + unit + " replace",
		"StopUnit " + unit + " replace",
		"StartUnit teamchat-server@masked.service replace",
		"EnableUnitFiles [" + unit + "] false false", "Reload",
		"DisableUnitFiles [" + unit + "] false", "Reload",
		"LoadUnit " + unit, "LoadUnit teamchat-server@other.service", "LoadUnit " + unit,
	}, "\n")
	if got := f.recorded(); got != want {
		t.Errorf("systemd saw:\n%s\nwant:\n%s", got, want)
	}

	redial(ctx, t, m, bus, lost, unit)
}

// wireJobs is every job and unit-file call against the fake systemd.
func wireJobs(ctx context.Context, t *testing.T, m *Manager, f *fakeSystemd, unit string) {
	t.Helper()
	h, hl := m.Host(), m.Health()
	if err := h.Start(ctx, unit); err != nil {
		t.Fatalf("start, signal before the reply: %v", err)
	}
	f.mu.Lock()
	f.after = true
	f.mu.Unlock()
	if err := hl.Restart(ctx, unit); err != nil {
		t.Fatalf("restart, signal racing the reply: %v", err)
	}
	f.mu.Lock()
	f.result, f.after = "failed", false
	f.mu.Unlock()
	if err := h.Stop(ctx, unit); !errors.Is(err, ErrJobFailed) || !strings.Contains(err.Error(), `"failed"`) {
		t.Fatalf("stop: %v", err)
	}
	if err := h.Start(ctx, "teamchat-server@masked.service"); !errors.Is(err, ErrMasked) ||
		err.Error() != "systemd start teamchat-server@masked.service: the unit is masked: Unit teamchat-server@masked.service is masked." {
		t.Fatalf("masked: %v", err)
	}
	if err := h.Enable(ctx, unit); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := h.Disable(ctx, unit); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

// wireStates reads the fake unit's properties as host and health see them.
func wireStates(ctx context.Context, t *testing.T, m *Manager, unit string) {
	t.Helper()
	h, hl := m.Host(), m.Health()
	st, err := h.State(ctx, unit)
	if err != nil || st.Active != "active" || !st.Enabled {
		t.Fatalf("host state = %+v, %v", st, err)
	}
	other, err := h.State(ctx, "teamchat-server@other.service")
	if err != nil || other.Active != inactive || other.Enabled {
		t.Fatalf("unknown unit's state = %+v, %v", other, err)
	}
	hs, err := hl.State(ctx, unit)
	if err != nil || hs.Sub != "running" || hs.NRestarts != 2 || hs.InvocationID != "0102030405060708090a0b0c0d0e0f10" ||
		!hs.ActiveSince.Equal(time.UnixMicro(1_000_000)) {
		t.Fatalf("health state = %+v, %v", hs, err)
	}
}

// redial stops the bus: the next call fails as unreachable, and the one after dials the new bus.
func redial(ctx context.Context, t *testing.T, m *Manager, bus scratch, lost <-chan struct{}, unit string) {
	t.Helper()
	h := m.Host()
	if err := bus.stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lost:
	case <-ctx.Done():
		t.Fatal("the lost bus was never seen")
	}
	if err := h.DaemonReload(ctx); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("reload on a lost bus: %v", err)
	}
	next := scratchBus(ctx, t)
	f2 := exportSystemd(t, next.address, unit)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", next.address)
	if err := h.DaemonReload(ctx); err != nil {
		t.Fatalf("reload on the new bus: %v", err)
	}
	if got := f2.recorded(); got != "Subscribe\nReload" {
		t.Errorf("the new systemd saw %q", got)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSystemBusThatCannotBeReached(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "absent"))
	m := NewManager(SystemBus, &execx.Fake{})
	err := m.Host().DaemonReload(t.Context())
	if !errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
