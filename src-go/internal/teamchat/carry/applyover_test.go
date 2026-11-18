package carry

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// SPEC-carry section 5 (SA): `team apply` over a tree `carry.Import` placed. The source is a
// team host.Apply made, sealed and exported; the guest imports it, runs `team carry-check`, and
// host.Apply runs over what is there, with fakes of systemd, firewalld, the network, readiness,
// the status record, the backup and the child's bootstrap.

// The address of the source's host and of a guest with other addresses.
const (
	saSourceAddr = "192.0.2.10"
	saOtherAddr  = "192.0.2.20"
)

// saInput is team ops declared on a host at address: its url is a name, so the approval holds on
// any guest; carried marks it as carried (SG6).
func saInput(t *testing.T, address string, carried bool) declare.Input {
	t.Helper()
	marker := ""
	if carried {
		marker = `,"carried_from":"the bus guest before its rebuild"`
	}
	in, err := declare.DecodeInput([]byte(`{"team":{"team":"ops","state":"present","port":8448,"description":"d",` +
		`"url":"https://chat.example.net:8448","networks":["192.0.2.0/24"],"humans":["alice"],"trust":{"kind":"github",` +
		`"org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}` + marker + `},"lxc_members":[],"address":"` + address + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// saHost is the seams host.Apply runs on under root: one host whose eth0 holds addr.
type saHost struct {
	t     *testing.T
	root  string
	in    declare.Input
	addr  netip.Addr
	units map[string]*host.UnitState
	// version is the Tuwunel version the host's play pins.
	version string
	// loaded is the server drop-in systemd read at its last reload; atBackup is the one the
	// homeserver ran when the backup was taken.
	loaded   string
	atBackup []string
	rules    map[bool]map[string]bool
	boots    []host.Bootstrap
	// rosterAtBoot is roster.json as the bootstrap found it.
	rosterAtBoot []byte
	refreshed    []bool
	calls        []string
}

func newSAHost(t *testing.T, root, addr string, in declare.Input) *saHost {
	t.Helper()
	return &saHost{
		t: t, root: root, in: in, addr: netip.MustParseAddr(addr), units: map[string]*host.UnitState{},
		rules: map[bool]map[string]bool{true: {}, false: {}}, version: "1.9.4",
	}
}

func (h *saHost) deps() host.Deps {
	h.t.Helper()
	root := h.root
	return host.Deps{
		Root: root, LockDir: root + paths.AdminRunDir, LockWait: host.PlayWait,
		Clock: &sealClock{now: time.UnixMilli(sealMS).Add(2 * time.Hour)}, Rand: rand.Reader,
		Pin:      host.Pin{Version: mustVersion(h.t, h.version), MemoryHigh: "512M", CacheCapacityModifier: "0.5"},
		Software: sealSoftware, CtlUID: os.Getuid(), CtlGID: os.Getgid(), StateOwnerUID: ownUID(h.t),
		Approvals: saApprovals(root),
		Systemd:   saSystemd{h}, Firewall: saFirewall{h}, Network: saNetwork{h}, Ready: saReady{h}, Status: saStatus{h},
		// The guards wire joins (SG3, SG2): a team carried and imported passes them.
		Guard: teamlock.All(restore.Guard(root), Guard(root), DrillImportGuard(root, ownUID(h.t))),
		Backup: func(_ context.Context, held *teamlock.Lock) error {
			if held == nil {
				h.t.Error("the backup was not given the held lock")
			}
			h.calls = append(h.calls, "backup")
			h.atBackup = append(h.atBackup, h.loaded)
			return nil
		},
		Bootstrap: func(_ context.Context, b host.Bootstrap) ([]string, error) {
			h.calls = append(h.calls, "bootstrap")
			h.boots = append(h.boots, b)
			raw, err := os.ReadFile(filepath.Clean(root + paths.RosterFile(b.Input.ID())))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			h.rosterAtBoot = raw
			return nil, nil
		},
	}
}

func (h *saHost) apply() ([]string, error) { return host.Apply(context.Background(), h.deps(), h.in) }

func (h *saHost) unit(name string) *host.UnitState {
	if h.units[name] == nil {
		h.units[name] = &host.UnitState{Active: "inactive"}
	}
	return h.units[name]
}

func (h *saHost) read(path string) []byte {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Clean(h.root + path))
	if err != nil {
		h.t.Fatal(err)
	}
	return raw
}

func (h *saHost) has(path string) bool {
	h.t.Helper()
	_, err := os.Lstat(h.root + path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		h.t.Fatal(err)
	}
	return err == nil
}

type saSystemd struct{ h *saHost }

func (s saSystemd) DaemonReload(context.Context) error {
	s.h.calls = append(s.h.calls, "daemon-reload")
	raw, err := os.ReadFile(filepath.Clean(s.h.root + paths.ServerDropInFile(s.h.in.ID())))
	if errors.Is(err, os.ErrNotExist) {
		s.h.loaded = ""
		return nil
	}
	s.h.loaded = string(raw)
	return err
}

func (s saSystemd) Start(_ context.Context, unit string) error {
	s.h.calls = append(s.h.calls, "start "+unit)
	s.h.unit(unit).Active = "active"
	return nil
}

func (s saSystemd) Restart(_ context.Context, unit string) error {
	s.h.calls = append(s.h.calls, "restart "+unit)
	s.h.unit(unit).Active = "active"
	return nil
}

func (s saSystemd) Stop(_ context.Context, unit string) error {
	s.h.calls = append(s.h.calls, "stop "+unit)
	s.h.unit(unit).Active = "inactive"
	return nil
}

func (s saSystemd) Enable(_ context.Context, unit string) error {
	s.h.calls = append(s.h.calls, "enable "+unit)
	s.h.unit(unit).Enabled = true
	return nil
}

func (s saSystemd) Disable(_ context.Context, unit string) error {
	s.h.calls = append(s.h.calls, "disable "+unit)
	s.h.unit(unit).Enabled = false
	return nil
}

func (s saSystemd) State(_ context.Context, unit string) (host.UnitState, error) {
	return *s.h.unit(unit), nil
}

func (s saSystemd) JournalTail(context.Context, string) (string, error) { return "", nil }

type saFirewall struct{ h *saHost }

func (f saFirewall) Zone(context.Context, netip.Prefix) (string, error) { return "public", nil }

func (f saFirewall) Query(_ context.Context, permanent bool, r host.Rule) (bool, error) {
	return f.h.rules[permanent][r.String()], nil
}

func (f saFirewall) Add(_ context.Context, permanent bool, r host.Rule) error {
	f.h.rules[permanent][r.String()] = true
	return nil
}

func (f saFirewall) Remove(_ context.Context, permanent bool, r host.Rule) error {
	delete(f.h.rules[permanent], r.String())
	return nil
}

type saNetwork struct{ h *saHost }

func (n saNetwork) Interfaces() ([]host.Interface, error) {
	return []host.Interface{
		{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "eth0", Addrs: []netip.Addr{n.h.addr}},
	}, nil
}

func (n saNetwork) Bridge() (netip.Prefix, error) {
	return netip.MustParsePrefix("198.51.100.0/24"), nil
}

// saApprovals is the approval store under root, production's (AG7).
func saApprovals(root string) declare.FileApprovals {
	return declare.FileApprovals{Path: func(t paths.TeamID) string { return root + paths.ApprovedFile(t) }, GID: -1}
}

// Route is eth0's for the declared network and the host's own address.
func (n saNetwork) Route(network netip.Prefix) (host.Route, bool, error) {
	if netip.MustParsePrefix("192.0.2.0/24").Contains(network.Addr()) {
		return host.Route{Interface: "eth0"}, true, nil
	}
	return host.Route{}, false, nil
}

var (
	saConfigAddress = regexp.MustCompile(`(?m)^address = \[(.*)\]$`)
	saExecListen    = regexp.MustCompile(`--listen (\S+)`)
)

// Listeners are what each active unit binds on the team's port: the homeserver every address of
// its config's address line, the terminator every --listen of its drop-in.
func (n saNetwork) Listeners() ([]host.Listener, error) {
	team, port := n.h.in.ID(), n.h.in.Team.Port
	var out []host.Listener
	bind := func(unit, addr string) {
		out = append(out, host.Listener{Addr: netip.MustParseAddrPort(net.JoinHostPort(addr, strconv.Itoa(port))), Unit: unit})
	}
	if n.h.unit(paths.ServerUnit(team)).Active == "active" {
		m := saConfigAddress.FindSubmatch(n.h.read(paths.ServerConfig(team)))
		if m == nil {
			n.h.t.Fatal("the installed config has no address line")
		}
		for quoted := range strings.SplitSeq(string(m[1]), ", ") {
			bind(paths.ServerUnit(team), strings.Trim(quoted, `"`))
		}
	}
	if n.h.unit(paths.TLSUnit(team)).Active == "active" {
		for _, m := range saExecListen.FindAllSubmatch(n.h.read(paths.TLSDropInFile(team)), -1) {
			bind(paths.TLSUnit(team), string(m[1]))
		}
	}
	return out, nil
}

type saReady struct{ h *saHost }

func (r saReady) ServerVersion(context.Context, int) error { return nil }

type saStatus struct{ h *saHost }

func (s saStatus) Refresh(_ context.Context, _ paths.TeamID, first bool, _ []string) error {
	s.h.refreshed = append(s.h.refreshed, first)
	return nil
}

func (s saStatus) Remove(context.Context, paths.TeamID) (string, error) {
	s.h.t.Error("the status record was removed")
	return "", errors.New("no remove")
}

// saApplied are the files host.Apply makes that the seal and the export read: copied from a
// first apply into the export world, they make its source a team host.Apply made.
var saApplied = []func(paths.TeamID) string{
	paths.TeamDeclaration, paths.ServerNameFile, paths.ServerConfig, paths.UnitsRecord, paths.CACertFile, paths.CAKeyFile,
}

// saWorld is a guest a sealed import of a team host.Apply made has landed on, `team carry-check`
// run, and the host it applies on.
type saWorld struct {
	*runWorld

	h *saHost
	// before is every carried file SA keeps, by path, as the guest held it before the apply.
	before map[string][]byte
}

// newSAWorld applies team ops on a source host at saSourceAddr, seals and exports it, imports it
// onto a guest whose eth0 is guestAddr, and runs the carry check there.
func newSAWorld(t *testing.T, guestAddr string) *saWorld {
	t.Helper()
	in := saInput(t, saSourceAddr, false)
	first := newSAHost(t, t.TempDir(), saSourceAddr, in)
	saInstalled(t, first.root)
	if err := os.MkdirAll(first.root+paths.StatusTeamDir(in.ID()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saApprovals(first.root).Save(in.ID(), in.Gated()); err != nil {
		t.Fatal(err)
	}
	if _, err := first.apply(); err != nil {
		t.Fatalf("the source's first apply: %v", err)
	}
	src := newSourcesWorld(t)
	src.decl = in.Team
	for _, path := range saApplied {
		p := path(src.team)
		info, err := os.Stat(first.root + p)
		if err != nil {
			t.Fatal(err)
		}
		src.putMode(p, string(first.read(p)), info.Mode().Perm())
	}
	src.put(paths.CtlState(src.team)+"/"+AdminTokenName, importToken)
	src.putMode(paths.ApprovedFile(src.team), string(declare.EncodeApproved(src.team, in.Gated())), 0o640)
	if _, err := src.seal(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.export(); err != nil {
		t.Fatal(err)
	}
	g := guestFor(t, src, src.archive(), KindSealed)
	// The leaf root the play makes on every host (the TLS state root, X31).
	if err := os.MkdirAll(g.root+paths.TLSStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := g.fresh(); err != nil {
		t.Fatalf("the import: %v", err)
	}
	cw := &checkWorld{runWorld: g, epoch: restore.Epoch{RestoreEpoch: 2, Rev: 5}}
	cw.reloads = true
	if _, err := cw.check(); err != nil {
		t.Fatalf("the carry check: %v", err)
	}
	w := &saWorld{runWorld: g, h: newSAHost(t, g.root, guestAddr, saInput(t, guestAddr, true)), before: map[string][]byte{}}
	team := w.team
	for _, p := range []string{
		paths.CACertFile(team), paths.CAKeyFile(team), paths.ServerNameFile(team), paths.RestoreEpochFile(team),
		paths.RosterFile(team), paths.ApprovedFile(team), paths.ServerConfig(team), paths.UnitsRecord(team),
	} {
		w.before[p] = w.read(p)
	}
	return w
}

// saInstalled is what the play installs on a host: both base units, as newGuest writes them.
func saInstalled(t *testing.T, root string) {
	t.Helper()
	for path, text := range map[string]string{paths.ServerUnitTemplateFile: "[Unit]\n", paths.TLSUnitTemplateFile: "[Unit]\n"} {
		if err := os.MkdirAll(filepath.Dir(root+path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root+path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{paths.AdminRunDir, paths.AdminStateRoot, paths.CtlStateRoot, paths.TLSStateRoot, paths.StatusRoot, paths.HomeserverPrivateStateRoot} {
		if err := os.MkdirAll(root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// kept fails for each file of paths the apply changed.
func (w *saWorld) kept(files ...string) {
	w.t.Helper()
	for _, p := range files {
		if !bytes.Equal(w.read(p), w.before[p]) {
			w.t.Errorf("%s is not the carried bytes after the apply", p)
		}
	}
}

// made fails for each file of paths that is not there.
func (w *saWorld) made(files ...string) {
	w.t.Helper()
	for _, p := range files {
		if !w.h.has(p) {
			w.t.Errorf("%s was not made", p)
		}
	}
}

// recordedDropIn is the server drop-in host.WriteRecordedDropIn makes from units, with the pin's
// memory bound.
func recordedDropIn(t *testing.T, team paths.TeamID, units host.Units) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+filepath.Dir(filepath.Dir(paths.ServerDropInFile(team))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteRecordedDropIn(root, team, units, "512M"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Clean(root + paths.ServerDropInFile(team)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// inOrder is whether every line of want is in got, in want's order.
func inOrder(got, want []string) bool {
	at := 0
	for _, line := range want {
		i := slices.Index(got[at:], line)
		if i < 0 {
			return false
		}
		at += i + 1
	}
	return true
}

// SA1, SG6: host.Apply of the declaration marked as carried, over the tree the import placed and
// the carry check left, keeps the CA pair, server-name, restore-epoch.json, the approval, the
// config and units.json byte for byte, gives the bootstrap the carried roster, and makes the
// leaf, both drop-ins, firewall.json and the stamp. No CA is made, no approval written, and on a
// guest that renders what the source did no backup is taken.
func TestApplyOverAnImportedTree(t *testing.T) {
	w := newSAWorld(t, saSourceAddr)
	team := w.team
	if w.h.has(paths.ServerDropInFile(team)) {
		t.Fatal("the carry check left its drop-in")
	}
	changed, err := w.h.apply()
	if err != nil {
		t.Fatalf("apply: %v (changed %q)", err, changed)
	}
	w.kept(paths.CACertFile(team), paths.CAKeyFile(team), paths.ServerNameFile(team), paths.RestoreEpochFile(team),
		paths.ApprovedFile(team), paths.ServerConfig(team), paths.UnitsRecord(team))
	if len(w.h.boots) != 1 || !bytes.Equal(w.h.rosterAtBoot, w.before[paths.RosterFile(team)]) {
		t.Errorf("the bootstrap ran %d times, the roster before it carried: %v", len(w.h.boots), bytes.Equal(w.h.rosterAtBoot, w.before[paths.RosterFile(team)]))
	}
	if key := host.RegKeyOf(w.before[paths.ServerConfig(team)]); len(w.h.boots) == 1 && w.h.boots[0].RegKey != key {
		t.Error("the bootstrap was not given the carried registration key")
	}
	w.made(paths.LeafCertFile(team), paths.LeafKeyFile(team), paths.ServerDropInFile(team), paths.TLSDropInFile(team),
		paths.FirewallRecord(team), paths.StampFile(team))
	w.servingAfter(changed)
}

// servingAfter holds what SA1's apply did beyond the files: only team.json of the records
// written, no CA, no first-apply files, no backup; both units active and enabled, the firewall
// rules added in both configurations, the status record refreshed as a first.
func (w *saWorld) servingAfter(changed []string) {
	w.t.Helper()
	t, team := w.t, w.team
	for _, line := range []string{"ca made", "backup taken", "files created", "config written", "units.json written"} {
		if slices.Contains(changed, line) {
			t.Errorf("the apply over an imported tree changed %q", line)
		}
	}
	if !slices.Contains(changed, "team.json written") {
		t.Errorf("team.json, now marked as carried, was not written: %q", changed)
	}
	file, err := declare.DecodeTeamFile(w.read(paths.TeamDeclaration(team)))
	if err != nil || file.CarriedFrom == "" {
		t.Errorf("team.json after the apply: %v, carried_from %q", err, file.CarriedFrom)
	}
	if slices.Contains(w.h.calls, "backup") {
		t.Error("a backup was taken though the restart set is the carried one")
	}
	for _, unit := range []string{paths.ServerUnit(team), paths.TLSUnit(team)} {
		if u := w.h.unit(unit); u.Active != "active" || !u.Enabled {
			t.Errorf("%s is %+v after the apply", unit, *u)
		}
	}
	if !slices.Equal(w.h.refreshed, []bool{true}) {
		t.Errorf("the status record was refreshed %v, not once as a first", w.h.refreshed)
	}
	// One rule for the declared network and one for the host's own address, in each configuration.
	if len(w.h.rules[true]) != 2 || len(w.h.rules[false]) != 2 {
		t.Errorf("the firewall holds %v", w.h.rules)
	}
}

// SA2: without units.json the apply fails at step backup before anything starts: the carried
// database's version is recorded nowhere else, which is why SX2 carries units.json.
func TestApplyOverAnImportedTreeNeedsUnitsJSON(t *testing.T) {
	w := newSAWorld(t, saSourceAddr)
	if err := os.Remove(w.root + paths.UnitsRecord(w.team)); err != nil {
		t.Fatal(err)
	}
	_, err := w.h.apply()
	var he *host.Error
	if !errors.As(err, &he) || he.Step != host.StepBackup ||
		!strings.Contains(err.Error(), "a database exists, and neither a drop-in nor units.json records the Tuwunel version that wrote it") {
		t.Fatalf("apply without units.json: %v", err)
	}
	for _, call := range w.h.calls {
		if strings.HasPrefix(call, "start ") || call == "backup" {
			t.Errorf("%s ran", call)
		}
	}
}

// SA3 (sc-D17): on a guest whose play pins another Tuwunel the restart set changes, so the apply
// first starts the carried database on the version units.json records, through the drop-in made
// from units.json, takes the backup, and only then installs this guest's drop-in, restarts on
// the pinned version and records it.
func TestApplyOverAnImportedTreeBacksUpOnTheRecordedVersion(t *testing.T) {
	w := newSAWorld(t, saSourceAddr)
	w.h.version = "1.9.5"
	team := w.team
	changed, err := w.h.apply()
	if err != nil {
		t.Fatalf("apply: %v (changed %q)", err, changed)
	}
	units, err := host.DecodeUnits(w.before[paths.UnitsRecord(team)], team)
	if err != nil {
		t.Fatal(err)
	}
	want := recordedDropIn(t, team, units)
	if len(w.h.atBackup) != 1 || w.h.atBackup[0] != want {
		t.Fatalf("the backup ran on %q, not the drop-in of units.json:\n%s", w.h.atBackup, want)
	}
	if !strings.Contains(want, "ExecStart="+paths.ServerBinary(units.Version)+"\n") {
		t.Errorf("the recorded drop-in does not name tuwunel %s", units.Version)
	}
	order := []string{
		"drop-in written for the backup, tuwunel " + units.Version.String(), "unit started for the backup", "backup taken",
		"drop-in written", "unit restarted", "units.json written",
	}
	if !inOrder(changed, order) {
		t.Errorf("the apply changed %q, not %q in that order", changed, order)
	}
	now, err := host.DecodeUnits(w.read(paths.UnitsRecord(team)), team)
	if err != nil || now.Version.String() != "1.9.5" {
		t.Errorf("units.json after the apply: %v, tuwunel %s", err, now.Version)
	}
	if !strings.Contains(string(w.read(paths.ServerDropInFile(team))), "ExecStart="+paths.ServerBinary(now.Version)+"\n") {
		t.Error("the drop-in after the apply does not run the pinned version")
	}
	w.kept(paths.CACertFile(team), paths.CAKeyFile(team), paths.ServerNameFile(team), paths.RestoreEpochFile(team), paths.ServerConfig(team))
}

// sc-D17: listen addresses are not in the restart set (the homeserver binds loopback alone,
// X18), so a guest with other addresses takes no backup; the apply records its listen set in
// units.json and the terminator's drop-in.
func TestApplyOverAnImportedTreeOnOtherAddresses(t *testing.T) {
	w := newSAWorld(t, saOtherAddr)
	team := w.team
	changed, err := w.h.apply()
	if err != nil {
		t.Fatalf("apply: %v (changed %q)", err, changed)
	}
	if slices.Contains(w.h.calls, "backup") || slices.Contains(changed, "config written") {
		t.Errorf("an apply on other addresses changed %q", changed)
	}
	now, err := host.DecodeUnits(w.read(paths.UnitsRecord(team)), team)
	if err != nil || !slices.Equal(now.Listen, []string{saOtherAddr}) {
		t.Errorf("units.json after the apply: %v, listening on %q, not this guest's address", err, now.Listen)
	}
	if !strings.Contains(string(w.read(paths.TLSDropInFile(team))), "--listen "+saOtherAddr) {
		t.Error("the terminator does not listen on this guest's address")
	}
}
