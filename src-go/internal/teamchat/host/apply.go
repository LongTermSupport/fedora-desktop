package host

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The steps a failure names (C 3.5.2, 3.5.3). StepRefusals is step 2 and StepRender step 3;
// StepFiles is step 4's first-apply files, and in `team remove` the records it deletes.
const (
	StepLock      = "lock"
	StepRefusals  = "refusals"
	StepRender    = "render"
	StepFiles     = "files"
	StepFirewall  = "firewall"
	StepBackup    = "backup"
	StepUnit      = "unit"
	StepReady     = "ready"
	StepAddresses = "addresses"
	StepBootstrap = "bootstrap"
	StepRecords   = "records"
	StepStatus    = "status"
	StepPurge     = "purge"
)

// The verbs an Error names.
const (
	verbApply  = "team apply"
	verbRemove = "team remove"
)

// The readiness wait (C 3.5.2 step 8).
const (
	readyWait = 60 * time.Second
	readyPoll = time.Second
)

// fileMax caps every file root reads back here: records, the config, the drop-in, the base unit.
const fileMax = 1 << 20

// UnitState is a unit's ActiveState and whether it is enabled.
type UnitState struct {
	Active  string
	Enabled bool
}

// active is systemd's ActiveState of a running unit.
const active = "active"

// Systemd is the unit manager.
type Systemd interface {
	DaemonReload(ctx context.Context) error
	Start(ctx context.Context, unit string) error
	Restart(ctx context.Context, unit string) error
	Stop(ctx context.Context, unit string) error
	Enable(ctx context.Context, unit string) error
	Disable(ctx context.Context, unit string) error
	State(ctx context.Context, unit string) (UnitState, error)
	JournalTail(ctx context.Context, unit string) (string, error)
	// CleanState is `systemctl clean --what=state` of the unit.
	CleanState(ctx context.Context, unit string) error
}

// Firewall is firewalld. Query answers by `--query-rich-rule`'s exit status, 0 present and 1
// absent, any other an error naming the code; permanent picks the configuration (C 3.2.5).
type Firewall interface {
	// Zone is the zone of the interface that routes source, or the default zone.
	Zone(ctx context.Context, source netip.Prefix) (string, error)
	Query(ctx context.Context, permanent bool, r Rule) (bool, error)
	Add(ctx context.Context, permanent bool, r Rule) error
	Remove(ctx context.Context, permanent bool, r Rule) error
}

// Interface is a network interface and its addresses.
type Interface struct {
	Name  string
	Addrs []netip.Addr
}

// Listener is a listening TCP socket and the unit whose process holds it ("" when none).
type Listener struct {
	Addr netip.AddrPort
	Unit string
}

// Network is the host's interfaces, its listening sockets (read from /proc/net/tcp and tcp6)
// and the live LXC bridge's subnet.
type Network interface {
	Interfaces() ([]Interface, error)
	Listeners() ([]Listener, error)
	Bridge() (netip.Prefix, error)
}

// Readiness is the probe of a homeserver's `/_tuwunel/server_version` on loopback.
type Readiness interface {
	ServerVersion(ctx context.Context, port int) error
}

// Status is the team's status record (C 3.11.3), whose writer is not this package's. Refresh
// rewrites it after an apply, first when the team had no stamp before (the record's origin is
// set then). Remove deletes it, or rewrites a migration-origin record to its pointer alone, and
// answers the CHANGED line, "" for none.
type Status interface {
	Refresh(ctx context.Context, team paths.TeamID, first bool) error
	Remove(ctx context.Context, team paths.TeamID) (string, error)
}

// Bootstrap is what step 10 is given: the input, the `team.json` this apply writes, and the
// registration key from the config for bootstrap step 1's admin account.
type Bootstrap struct {
	Input    declare.Input
	TeamFile []byte
	RegKey   string
}

// Deps is what `team apply` and `team remove` use. Root prefixes every host path ("" on a
// host); LockDir is paths.AdminRunDir on a host; the owner ids are teamchat-ctl's, for
// ctl/<team>/. Backup takes the team's backup under the held lock; Bootstrap runs step 10 in
// the teamchat-ctl child and answers its CHANGED lines.
type Deps struct {
	Root, LockDir  string
	LockWait       time.Duration
	Clock          Clock
	Rand           io.Reader
	Pin            Pin
	Software       string
	CtlUID, CtlGID int
	Approvals      declare.Approvals
	Systemd        Systemd
	Firewall       Firewall
	Network        Network
	Ready          Readiness
	Status         Status
	Backup         func(ctx context.Context, held *Lock) error
	Bootstrap      func(ctx context.Context, b Bootstrap) ([]string, error)
}

// Error is a failed verb: the step, and whether it is drift a rule refused (exit 70); anything
// else is 1, except a BusyError it wraps (75).
type Error struct {
	Verb, Step string
	Drift      bool
	Err        error
}

func (e *Error) Error() string { return e.Verb + ", step " + e.Step + ": " + e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

// answer is step 3's five-way answer.
type answer int

const (
	answerNothing answer = iota
	answerWrite
	answerReload
	answerRestart
	answerFirst
)

// decide is step 3's answer: no drop-in is a first start, no units.json or a changed restart
// set a restart, a changed reload set a reload, a changed `team.json` a write alone.
func decide(dropIn bool, units *unitsRecord, r rendered, teamFile []byte) answer {
	switch {
	case !dropIn:
		return answerFirst
	case units == nil || !r.digests.sameRestart(units.digests):
		return answerRestart
	case !r.digests.sameReload(units.digests):
		return answerReload
	case string(teamFile) != string(r.teamFile):
		return answerWrite
	}
	return answerNothing
}

type applying struct {
	d       Deps
	in      declare.Input
	team    paths.TeamID
	unit    string
	lock    *Lock
	changed []string

	first, stamped bool
	ifaces         map[string]string
	regKey         string
	r              rendered
	answer         answer
	units          *unitsRecord
	config, dropIn []byte
	haveDropIn     bool
	teamFile       []byte
	allowFrom      []string
}

// Apply is `team apply`'s steps 1 to 11 for one decoded input (C 3.5.2); step 10 is
// Deps.Bootstrap. It answers the CHANGED lines, those before a failure included.
func Apply(ctx context.Context, d Deps, in declare.Input) ([]string, error) {
	a := &applying{d: d, in: in, team: in.ID(), unit: paths.ServerUnit(in.ID())}
	lock, err := TakeLock(ctx, d.LockDir, a.team, "team.apply", d.LockWait, d.Clock)
	if err != nil {
		return nil, a.fail(StepLock, err)
	}
	a.lock = lock
	err = a.run(ctx)
	if rerr := lock.Release(); rerr != nil && err == nil {
		err = a.fail(StepLock, rerr)
	}
	return a.changed, err
}

func (a *applying) fail(step string, err error) error {
	return &Error{Verb: verbApply, Step: step, Err: err}
}

func (a *applying) refuse(format string, args ...any) error {
	return &Error{Verb: verbApply, Step: StepRefusals, Drift: true, Err: fmt.Errorf(format, args...)}
}

func (a *applying) note(line string) { a.changed = append(a.changed, line) }

func (a *applying) path(p string) string { return a.d.Root + p }

func (a *applying) run(ctx context.Context) error {
	for _, step := range []func(context.Context) error{
		a.refusals, a.render, a.files, a.firewall, a.backup, a.start, a.waitReady, a.addresses,
		a.bootstrap, a.records,
	} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

// refusals is step 2: nothing changes before these hold.
func (a *applying) refusals(_ context.Context) error {
	name, found, err := state.ReadFile(a.path(paths.ServerNameFile(a.team)), fileMax)
	if err != nil {
		return a.fail(StepRefusals, err)
	}
	a.first = !found
	if found && string(name) != a.in.Team.ServerName {
		return a.refuse("server_name %s is not %s, fixed at the team's first apply (C3)", a.in.Team.ServerName, name)
	}
	approved, err := a.d.Approvals.Load(a.team)
	if err != nil {
		return a.fail(StepRefusals, err)
	}
	if err := declare.Hold(approved, a.in); err != nil {
		return &Error{Verb: verbApply, Step: StepRefusals, Drift: true, Err: err}
	}
	if err := a.listenInterfaces(); err != nil {
		return err
	}
	return a.port()
}

// listenInterfaces holds each listen address to exactly one interface, and none a wildcard.
func (a *applying) listenInterfaces() error {
	ifaces, err := a.d.Network.Interfaces()
	if err != nil {
		return a.fail(StepRefusals, err)
	}
	a.ifaces = map[string]string{}
	for _, listen := range a.in.Team.Listen {
		addr, err := netip.ParseAddr(listen)
		if err != nil || addr.IsUnspecified() {
			return a.refuse("listen address %s is a wildcard or not an address", listen)
		}
		var on []string
		for _, i := range ifaces {
			if slices.Contains(i.Addrs, addr) {
				on = append(on, i.Name)
			}
		}
		switch {
		case len(on) == 0:
			return a.refuse("listen address %s is on no interface", listen)
		case len(on) > 1:
			return a.refuse("listen address %s is on more than one interface: %s", listen, strings.Join(on, ", "))
		case !validInterface(on[0]):
			return a.fail(StepRefusals, fmt.Errorf("interface %q of %s has no device unit name", on[0], listen))
		}
		a.ifaces[listen] = on[0]
	}
	return nil
}

// port refuses a port another v2 team's units.json records, or one listening that is not this
// team's homeserver.
func (a *applying) port() error {
	port := a.in.Team.Port
	other, taken, err := a.portRecordedBy(port)
	if err != nil {
		return a.fail(StepRefusals, err)
	}
	if taken {
		return a.refuse("port %d is recorded by team %s's units.json", port, other)
	}
	listeners, err := a.d.Network.Listeners()
	if err != nil {
		return a.fail(StepRefusals, err)
	}
	for _, l := range listeners {
		if int(l.Addr.Port()) == port && l.Unit != a.unit {
			holder := l.Unit
			if holder == "" {
				holder = "a process of no unit"
			}
			return a.refuse("port %d is listening on %s, held by %s, not %s", port, l.Addr, holder, a.unit)
		}
	}
	return nil
}

// portRecordedBy is the other v2 team whose units.json records port, and whether there is one.
func (a *applying) portRecordedBy(port int) (paths.TeamID, bool, error) {
	var none paths.TeamID
	entries, err := os.ReadDir(a.path(paths.AdminStateRoot))
	if err != nil {
		return none, false, err
	}
	for _, e := range entries {
		other, perr := paths.ParseTeamID(e.Name())
		if perr != nil || other == a.team || !e.IsDir() {
			continue
		}
		raw, found, rerr := state.ReadFile(a.path(paths.UnitsRecord(other)), fileMax)
		if rerr != nil {
			return none, false, rerr
		}
		if !found {
			continue
		}
		u, derr := decodeUnits(raw, other)
		if derr != nil {
			return none, false, derr
		}
		if u.port == port {
			return other, true, nil
		}
	}
	return none, false, nil
}

// render is step 3: the files in memory, what is installed, and the answer.
func (a *applying) render(_ context.Context) error {
	base, found, err := state.ReadFile(a.path(paths.ServerUnitTemplateFile), fileMax)
	if err == nil && !found {
		err = fmt.Errorf("the base unit %s is not installed", paths.ServerUnitTemplateFile)
	}
	if err != nil {
		return a.fail(StepRender, err)
	}
	if err = a.readInstalled(); err == nil {
		err = a.allowed()
	}
	if err != nil {
		return a.fail(StepRender, err)
	}
	var devices []string
	for _, listen := range a.in.Team.Listen {
		if unit := deviceUnit(a.ifaces[listen]); !slices.Contains(devices, unit) {
			devices = append(devices, unit)
		}
	}
	a.r, err = render(renderSpec{
		team: a.in.Team, allowFrom: a.allowFrom, devices: devices, regKey: a.regKey, pin: a.d.Pin, baseUnit: base,
	})
	if err != nil {
		return a.fail(StepRender, err)
	}
	a.answer = decide(a.haveDropIn, a.units, a.r, a.teamFile)
	return nil
}

// readInstalled reads the installed config (and its registration key, or makes a new one),
// drop-in, units.json and team.json.
func (a *applying) readInstalled() error {
	var found bool
	var err error
	if a.config, found, err = state.ReadFile(a.path(paths.ServerConfig(a.team)), fileMax); err != nil {
		return err
	}
	if found {
		if a.regKey = regKeyOf(a.config); a.regKey == "" {
			return errors.New("the installed config holds no registration key")
		}
	} else {
		key := make([]byte, 64)
		if _, err = io.ReadFull(a.d.Rand, key); err != nil {
			return fmt.Errorf("cannot make the registration key: %w", err)
		}
		a.regKey = hex.EncodeToString(key)
	}
	if a.dropIn, a.haveDropIn, err = state.ReadFile(a.path(paths.ServerDropInFile(a.team)), fileMax); err != nil {
		return err
	}
	raw, found, err := state.ReadFile(a.path(paths.UnitsRecord(a.team)), fileMax)
	if err != nil {
		return err
	}
	if found {
		u, derr := decodeUnits(raw, a.team)
		if derr != nil {
			return derr
		}
		a.units = &u
	}
	a.teamFile, _, err = state.ReadFile(a.path(paths.TeamDeclaration(a.team)), fileMax)
	return err
}

// allowed is allow_from, with the live LXC bridge's subnet for a team with an LXC member.
func (a *applying) allowed() error {
	a.allowFrom = slices.Clone(a.in.Team.AllowFrom)
	if !slices.ContainsFunc(a.in.LXC, func(m declare.LXCMember) bool { return m.State == declare.StatePresent }) {
		return nil
	}
	bridge, err := a.d.Network.Bridge()
	if err != nil {
		return err
	}
	if s := bridge.Masked().String(); !slices.Contains(a.allowFrom, s) {
		a.allowFrom = append(a.allowFrom, s)
	}
	return nil
}

// files is step 4, on a first apply only: the team's directories, the config with its new
// registration key, and the server name, each created without replacing anything.
func (a *applying) files(_ context.Context) error {
	if !a.first {
		return nil
	}
	err := mkdirOwned(a.path(paths.AdminState(a.team)), -1, -1)
	if err == nil {
		err = mkdirOwned(a.path(paths.CtlState(a.team)), a.d.CtlUID, a.d.CtlGID)
	}
	if err == nil && a.config == nil {
		if err = createOnly(a.path(paths.ServerConfig(a.team)), a.r.config, 0o600); err == nil {
			a.config = a.r.config
		}
	}
	if err == nil {
		err = createOnly(a.path(paths.ServerNameFile(a.team)), []byte(a.in.Team.ServerName), 0o600)
	}
	if err != nil {
		return a.fail(StepFiles, err)
	}
	a.note("files created")
	return nil
}

// firewall is step 5 (H 7.11): the union recorded, the new rules added, the old ones removed,
// the new set recorded alone.
func (a *applying) firewall(ctx context.Context) error {
	if err := a.converge(ctx); err != nil {
		return a.fail(StepFirewall, err)
	}
	return nil
}

func (a *applying) converge(ctx context.Context) error {
	record := a.path(paths.FirewallRecord(a.team))
	old, found, err := readRules(record, a.team)
	if err != nil {
		return err
	}
	wanted, err := a.wantedRules(ctx)
	if err != nil {
		return err
	}
	same := found && slices.Equal(old, wanted)
	if !same {
		if err := state.WriteFile(record, encodeFirewall(a.team, union(old, wanted)), 0o600, -1); err != nil {
			return err
		}
	}
	for _, r := range wanted {
		if err := a.ensure(ctx, r, true); err != nil {
			return err
		}
	}
	for _, r := range old {
		if !slices.Contains(wanted, r) {
			if err := a.ensure(ctx, r, false); err != nil {
				return err
			}
		}
	}
	if same {
		return nil
	}
	return state.WriteFile(record, encodeFirewall(a.team, wanted), 0o600, -1)
}

// readRules reads firewall.json, and whether it is there.
func readRules(record string, team paths.TeamID) ([]Rule, bool, error) {
	raw, found, err := state.ReadFile(record, fileMax)
	if err != nil || !found {
		return nil, false, err
	}
	rules, err := decodeFirewall(raw, team)
	if err != nil {
		return nil, false, err
	}
	return rules, true, nil
}

// wantedRules is one rule per allow_from source, in the zone that routes it.
func (a *applying) wantedRules(ctx context.Context) ([]Rule, error) {
	wanted := make([]Rule, 0, len(a.allowFrom))
	for _, s := range a.allowFrom {
		source, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		zone, err := a.d.Firewall.Zone(ctx, source)
		if err != nil {
			return nil, err
		}
		wanted = append(wanted, Rule{Zone: zone, Source: source, Port: a.in.Team.Port})
	}
	return wanted, nil
}

// union is old with each rule of wanted it lacks appended.
func union(old, wanted []Rule) []Rule {
	all := slices.Clone(old)
	for _, r := range wanted {
		if !slices.Contains(all, r) {
			all = append(all, r)
		}
	}
	return all
}

// ensure makes r present (or absent) in the permanent and the runtime configuration, each
// queried and converged on its own.
func (a *applying) ensure(ctx context.Context, r Rule, present bool) error {
	n, err := ensureRule(ctx, a.d.Firewall, r, present)
	if n > 0 && present {
		a.note("firewall added " + r.String())
	}
	if n > 0 && !present {
		a.note("firewall removed " + r.String())
	}
	return err
}

// ensureRule converges one rule in both configurations and answers how many it changed.
func ensureRule(ctx context.Context, fw Firewall, r Rule, present bool) (int, error) {
	n := 0
	for _, permanent := range []bool{true, false} {
		is, err := fw.Query(ctx, permanent, r)
		if err != nil {
			return n, err
		}
		switch {
		case present && !is:
			err = fw.Add(ctx, permanent, r)
		case !present && is:
			err = fw.Remove(ctx, permanent, r)
		default:
			continue
		}
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// backup is step 6: a team with a database whose restart set changes is backed up first, by
// the binary that wrote the database.
func (a *applying) backup(ctx context.Context) error {
	db, err := isDir(a.path(paths.HomeserverPrivateDatabase(a.team)))
	if err != nil {
		return a.fail(StepBackup, err)
	}
	if !db || a.units != nil && a.r.digests.sameRestart(a.units.digests) {
		return nil
	}
	if err := a.startForBackup(ctx); err != nil {
		return a.fail(StepBackup, err)
	}
	if err := a.d.Backup(ctx, a.lock); err != nil {
		return a.fail(StepBackup, err)
	}
	a.note("backup taken")
	return nil
}

// startForBackup starts an inactive homeserver on the version units.json records, writing that
// drop-in when `team remove` deleted it, and waits for it.
func (a *applying) startForBackup(ctx context.Context) error {
	st, err := a.d.Systemd.State(ctx, a.unit)
	if err != nil || st.Active == active {
		return err
	}
	if !a.haveDropIn && a.units == nil {
		return errors.New("a database exists, and neither a drop-in nor units.json records the Tuwunel version that wrote it")
	}
	port := a.in.Team.Port
	if a.units != nil {
		port = a.units.port
	}
	if !a.haveDropIn {
		old := dropInSpec{
			team: a.team, version: a.units.version, memoryHigh: a.d.Pin.MemoryHigh, port: a.units.port, listen: a.units.listen,
		}
		if err := a.writeDropIn(ctx, old.text()); err != nil {
			return err
		}
		a.note("drop-in written for the backup, tuwunel " + a.units.version.String())
	}
	if err := a.d.Systemd.Start(ctx, a.unit); err != nil {
		return err
	}
	a.note("unit started for the backup")
	return a.ready(ctx, port)
}

func (a *applying) writeDropIn(ctx context.Context, text []byte) error {
	file := a.path(paths.ServerDropInFile(a.team))
	if err := mkdirShared(filepath.Dir(file)); err != nil {
		return err
	}
	if err := state.WriteFile(file, text, 0o644, -1); err != nil {
		return err
	}
	a.dropIn, a.haveDropIn = text, true
	return a.d.Systemd.DaemonReload(ctx)
}

// start is step 7: the config and drop-in written, then the unit started, restarted or left
// running, and enabled.
func (a *applying) start(ctx context.Context) error {
	if err := a.converge7(ctx); err != nil {
		return a.fail(StepUnit, err)
	}
	return nil
}

func (a *applying) converge7(ctx context.Context) error {
	if string(a.config) != string(a.r.config) {
		if err := state.WriteFile(a.path(paths.ServerConfig(a.team)), a.r.config, 0o600, -1); err != nil {
			return err
		}
		a.note("config written")
	}
	if string(a.dropIn) != string(a.r.dropIn) {
		if err := a.writeDropIn(ctx, a.r.dropIn); err != nil {
			return err
		}
		a.note("drop-in written")
	}
	st, err := a.d.Systemd.State(ctx, a.unit)
	if err != nil {
		return err
	}
	running := st.Active == active
	switch {
	case running && (a.answer == answerFirst || a.answer == answerRestart):
		err = a.d.Systemd.Restart(ctx, a.unit)
		a.noteIf(err, "unit restarted")
	case !running:
		err = a.d.Systemd.Start(ctx, a.unit)
		a.noteIf(err, "unit started")
	}
	if err != nil || st.Enabled {
		return err
	}
	err = a.d.Systemd.Enable(ctx, a.unit)
	a.noteIf(err, "unit enabled")
	return err
}

func (a *applying) noteIf(err error, line string) {
	if err == nil {
		a.note(line)
	}
}

// waitReady is step 8.
func (a *applying) waitReady(ctx context.Context) error {
	if err := a.ready(ctx, a.in.Team.Port); err != nil {
		return a.fail(StepReady, err)
	}
	return nil
}

// ready waits up to 60 s for the unit to be active and its server_version to answer. A unit
// that is neither active nor activating fails at once; its journal tail is only explanation,
// the verdict is the state (H 7.13).
func (a *applying) ready(ctx context.Context, port int) error {
	start := a.d.Clock.Now()
	var last error
	for {
		st, err := a.d.Systemd.State(ctx, a.unit)
		if err != nil {
			return err
		}
		switch st.Active {
		case active:
			if last = a.d.Ready.ServerVersion(ctx, port); last == nil {
				return nil
			}
		case "activating", "reloading":
			last = fmt.Errorf("%s is %s", a.unit, st.Active)
		default:
			tail, jerr := a.d.Systemd.JournalTail(ctx, a.unit)
			if jerr != nil {
				return fmt.Errorf("%s is %s; its journal cannot be read: %w", a.unit, st.Active, jerr)
			}
			return fmt.Errorf("%s is %s; the journal's last lines, as explanation only:\n%s", a.unit, st.Active, tail)
		}
		if a.d.Clock.Now().Sub(start) >= readyWait {
			return fmt.Errorf("%s is not ready within %v: %w", a.unit, readyWait, last)
		}
		if err := a.d.Clock.Sleep(ctx, readyPoll); err != nil {
			return err
		}
	}
}

// addresses is step 9: the bound address set is loopback and listen exactly (H 7.21).
func (a *applying) addresses(_ context.Context) error {
	listeners, err := a.d.Network.Listeners()
	if err != nil {
		return a.fail(StepAddresses, err)
	}
	want := []string{loopbackListen}
	for _, l := range a.in.Team.Listen {
		if !slices.Contains(want, l) {
			want = append(want, l)
		}
	}
	var got []string
	for _, l := range listeners {
		if l.Unit == a.unit && int(l.Addr.Port()) == a.in.Team.Port && !slices.Contains(got, l.Addr.Addr().String()) {
			got = append(got, l.Addr.Addr().String())
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return a.fail(StepAddresses, fmt.Errorf("%s is bound to %q, not %q", a.unit, got, want))
	}
	return nil
}

// bootstrap is step 10, in the teamchat-ctl child.
func (a *applying) bootstrap(ctx context.Context) error {
	lines, err := a.d.Bootstrap(ctx, Bootstrap{Input: a.in, TeamFile: a.r.teamFile, RegKey: a.regKey})
	a.changed = append(a.changed, lines...)
	if err != nil {
		return a.fail(StepBootstrap, err)
	}
	return nil
}

// records is step 11: team.json and units.json, then the stamp last, then the status record.
func (a *applying) records(ctx context.Context) error {
	if err := a.writeRecords(); err != nil {
		return a.fail(StepRecords, err)
	}
	if err := a.d.Status.Refresh(ctx, a.team, !a.stamped); err != nil {
		return a.fail(StepRecords, err)
	}
	return nil
}

func (a *applying) writeRecords() error {
	if err := a.writeIfChanged(paths.TeamDeclaration(a.team), a.teamFile, a.r.teamFile, 0o600, "team.json written"); err != nil {
		return err
	}
	u := unitsRecord{digests: a.r.digests, version: a.d.Pin.Version, port: a.in.Team.Port, listen: a.in.Team.Listen}
	var was []byte
	if a.units != nil {
		was = a.units.encode(a.team)
	}
	if err := a.writeIfChanged(paths.UnitsRecord(a.team), was, u.encode(a.team), 0o600, "units.json written"); err != nil {
		return err
	}
	return a.writeStamp()
}

func (a *applying) writeIfChanged(p string, was, is []byte, mode fs.FileMode, line string) error {
	if string(was) == string(is) {
		return nil
	}
	if err := state.WriteFile(a.path(p), is, mode, -1); err != nil {
		return err
	}
	a.note(line)
	return nil
}

func (a *applying) writeStamp() error {
	file := a.path(paths.StampFile(a.team))
	s := stamp{
		config: digest(a.r.teamFile), software: a.d.Software, tuwunel: a.d.Pin.Version.String(),
		appliedMS: a.d.Clock.Now().UnixMilli(),
	}
	raw, found, err := state.ReadFile(file, fileMax)
	if err != nil {
		return err
	}
	a.stamped = found
	if found {
		was, err := decodeStamp(raw, a.team)
		if err != nil {
			return err
		}
		if was.sameAs(s) {
			return nil
		}
	}
	for _, dir := range []string{filepath.Dir(paths.StatusTeamDir(a.team)), paths.StatusTeamDir(a.team)} {
		if err := mkdirShared(a.path(dir)); err != nil {
			return err
		}
	}
	if err := state.WriteFile(file, s.encode(a.team), 0o644, -1); err != nil {
		return err
	}
	a.note("stamp written")
	return nil
}
