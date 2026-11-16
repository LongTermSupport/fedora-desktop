package admin

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// discarding is the engine whose discard removes the record, as restore.Discard does.
type discarding struct {
	*fakeRestorer

	root string
}

func (d discarding) Discard(ctx context.Context, lock *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	out, err := d.fakeRestorer.Discard(ctx, lock, team)
	if err != nil {
		return out, err
	}
	return out, os.Remove(d.root + paths.RestoreRecord(team))
}

// drillHost is a host holding one marked scratch team, its restore recorded.
func drillHost(t *testing.T) (*restoreWorld, HostDrill, paths.TeamID) {
	t.Helper()
	bw := bareWorld(t)
	w := &restoreWorld{t: t, root: bw.root, sys: bw.sys()}
	w.engine = &fakeRestorer{t: t, sys: w.sys}
	id := scratchTeam(t)
	if err := os.MkdirAll(w.root+paths.AdminStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := drill.WriteMarker(w.root, id, drill.Marker{Digest: mustDigest(t), StartedMS: t0ms}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+paths.RestoreRecord(id), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := HostDrill{Sys: w.sys, Engine: func(context.Context) (Restorer, error) {
		w.engines++
		return discarding{w.engine, w.root}, nil
	}}
	return w, h, id
}

func mustDigest(t *testing.T) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// --clean discards a scratch team's recorded restore through the engine, under the team lock,
// without the proof a restore needs, then removes the team with purge.
func TestHostDrillCleanDiscardsThenPurges(t *testing.T) {
	w, h, id := drillHost(t)
	cleaned, err := h.Clean(t.Context())
	if err != nil || !slices.Equal(cleaned, []paths.TeamID{id}) {
		t.Fatalf("Clean = %v, %v", cleaned, err)
	}
	if !slices.Equal(w.engine.calls, []string{"discard " + id.String()}) {
		t.Errorf("engine calls = %q", w.engine.calls)
	}
	if _, err := os.Stat(w.root + paths.AdminState(id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch team's state is still there: %v", err)
	}
}

func markerKept(t *testing.T, w *restoreWorld, id paths.TeamID) {
	t.Helper()
	if _, err := os.Stat(w.root + paths.DrillMarker(id)); err != nil {
		t.Errorf("the marker is gone: %v", err)
	}
}

// A discard that fails stops the clean-up, and the team stays for the next --clean.
func TestHostDrillCleanStopsAtAFailedDiscard(t *testing.T) {
	w, h, id := drillHost(t)
	w.engine.err = errors.New("the discard failed")
	cleaned, err := h.Clean(t.Context())
	if err == nil || !strings.Contains(err.Error(), "the discard failed") || len(cleaned) != 0 {
		t.Fatalf("Clean = %v, %v", cleaned, err)
	}
	markerKept(t, w, id)
}

// busyNetwork is the host's network with one more socket listening.
type busyNetwork struct {
	host.Network

	busy host.Listener
}

func (n busyNetwork) Listeners() ([]host.Listener, error) {
	l, err := n.Network.Listeners()
	return append(l, n.busy), err
}

// drillRun is a HostDrill whose drill records the config and the rig it is given.
type drillRun struct {
	cfg    drill.Config
	rig    drill.Rig
	runs   int
	keyErr error
}

func (r *drillRun) wire(t *testing.T, h *HostDrill) restore.Deps {
	t.Helper()
	deps := restore.Deps{Root: h.Sys.Host.Root, StateOwnerUID: h.Sys.Host.StateOwnerUID}
	h.Deps = func(context.Context) (restore.Deps, error) { return deps, nil }
	v, err := paths.ParseServerVersion("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	h.Key = func(context.Context) (restore.Key, error) {
		return restore.Key{Software: mustDigest(t), Tuwunel: v, Systemd: "256.7-1.fc41"}, r.keyErr
	}
	h.TakeBackup = func(context.Context, *teamlock.Lock) (int64, error) { return 1, nil }
	h.Dial = func(context.Context, netip.AddrPort) (bool, error) { return false, nil }
	h.Drill = func(_ context.Context, cfg drill.Config, rig drill.Rig) (drill.Outcome, error) {
		r.runs++
		r.cfg, r.rig = cfg, rig
		return drill.Outcome{Written: true}, nil
	}
	return deps
}

// Run is the drill on a fresh rig over the engine's Deps, its hooks unset, with the config of
// this host, the proof's key, where it runs and the drill port.
func TestHostDrillRunDrivesTheDrillOnTheRig(t *testing.T) {
	w, h, _ := drillHost(t)
	r := &drillRun{}
	deps := r.wire(t, &h)
	out, err := h.Run(t.Context(), 8470, restore.WhereVMLab)
	if err != nil || !out.Written || r.runs != 1 {
		t.Fatalf("Run = %+v, %v, %d runs", out, err, r.runs)
	}
	if c := r.cfg; c.Root != w.root || c.UID != deps.StateOwnerUID || c.Port != 8470 || c.Where != restore.WhereVMLab ||
		c.Key.Tuwunel.String() != "v1.2.3" || c.Key.Systemd != "256.7-1.fc41" || c.Rand == nil || c.Clock == nil {
		t.Errorf("config = %+v", c)
	}
	rigOK(t, r.rig, deps)
}

// rigOK holds that rig is a whole DrillRig over deps, the engine's hooks unset, reaching the
// scratch team's server on loopback.
func rigOK(t *testing.T, r drill.Rig, deps restore.Deps) {
	t.Helper()
	rig, ok := r.(*DrillRig)
	if !ok {
		t.Fatalf("rig = %T", r)
	}
	if rig.Engine.OnStaged != nil || rig.Engine.AfterSwap != nil || rig.Engine.Root != deps.Root {
		t.Errorf("the rig's engine = %+v", rig.Engine)
	}
	if missing := nilRigFields(rig); len(missing) != 0 {
		t.Errorf("the rig leaves %q unset", missing)
	}
	if got := rig.Base(8470); got != "http://127.0.0.1:8470" {
		t.Errorf("Base = %q", got)
	}
}

// nilRigFields names the rig's func and pointer fields left nil.
func nilRigFields(rig *DrillRig) []string {
	v := reflect.ValueOf(rig).Elem()
	var out []string
	for i := range v.NumField() {
		f := v.Field(i)
		k := f.Kind()
		if v.Type().Field(i).IsExported() && (k == reflect.Func || k == reflect.Interface || k == reflect.Pointer) && f.IsNil() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	return out
}

// A drill port anything listens on, at any address, is refused (70) before the key is read or
// the drill runs, naming the socket and its unit.
func TestHostDrillRunRefusesAPortInUse(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8470", "[::]:8470", "192.0.2.10:8470"} {
		w, h, id := drillHost(t)
		r := &drillRun{}
		r.wire(t, &h)
		w.sys.Host.Network = busyNetwork{w.sys.Host.Network, host.Listener{Addr: netip.MustParseAddrPort(addr), Unit: "other.service"}}
		out, err := h.Run(t.Context(), 8470, restore.WhereHost)
		if !errors.Is(err, drill.ErrRefused) || ExitCodeOf(err) != ExitRefused || !strings.Contains(err.Error(), "8470 is in use") ||
			!strings.Contains(err.Error(), "other.service") || r.runs != 0 || out.Written {
			t.Errorf("%s: Run = %+v, %v, %d runs", addr, out, err, r.runs)
		}
		markerKept(t, w, id)
	}
}

// Another port in use is no refusal.
func TestHostDrillRunIgnoresOtherPorts(t *testing.T) {
	w, h, _ := drillHost(t)
	r := &drillRun{}
	r.wire(t, &h)
	w.sys.Host.Network = busyNetwork{w.sys.Host.Network, host.Listener{Addr: netip.MustParseAddrPort("127.0.0.1:8471")}}
	if _, err := h.Run(t.Context(), 8470, restore.WhereHost); err != nil || r.runs != 1 {
		t.Fatalf("Run = %v, %d runs", err, r.runs)
	}
}

// A key that cannot be read stops the drill before it runs.
func TestHostDrillRunNeedsTheKey(t *testing.T) {
	_, h, _ := drillHost(t)
	r := &drillRun{keyErr: errors.New("no systemd version")}
	r.wire(t, &h)
	if _, err := h.Run(t.Context(), 8470, restore.WhereHost); err == nil || !strings.Contains(err.Error(), "no systemd version") || r.runs != 0 {
		t.Fatalf("Run = %v, %d runs", err, r.runs)
	}
}
