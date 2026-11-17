package carry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// sealMS is the seal world's clock.
const sealMS = 1_700_000_000_000

// The seal world's software digest, units.json's digests and the line its status record's
// removal answers.
const (
	sealSoftware   = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"
	unitsDigestHex = "0000000000000000000000000000000000000000000000000000000000000001"
	statusRemoved  = "status record removed"
)

// sealUnit is one unit as the fakes show it.
type sealUnit struct {
	active  string
	enabled bool
	loaded  bool
}

// sealWorld is a host for the seal: a temp tree under restore.OS, and a fake for every other
// seam. Every call through the units, systemd, readiness, child and status seams, and every
// write or removal through the FS, is one call in calls; the kill-th ends the seal's goroutine
// as a kill would (SM5), before it takes effect.
type sealWorld struct {
	t    *testing.T
	team paths.TeamID
	root string
	uid  uint32
	decl declare.Team

	mu    sync.Mutex
	calls []string
	n     int
	kill  int
	// fail is the call that answers errInjected (fault), 0 for none.
	fail int

	units map[string]*sealUnit
	// stuck keeps the homeserver activating after its stop, as a unit systemd restarts would.
	stuck bool
	// reloads admits daemon-reload, which only the carry check calls, failing it with reloadErr.
	reloads   bool
	reloadErr error
	// silent is a homeserver that does not answer server_version.
	silent  bool
	tail    Tail
	tailErr error
	clock   *sealClock
}

type sealClock struct {
	mu    sync.Mutex
	now   time.Time
	slept time.Duration
}

func (c *sealClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *sealClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.slept += d
	return nil
}

func newSealWorld(t *testing.T) *sealWorld {
	t.Helper()
	team := sampleTeam(t)
	w := &sealWorld{
		t: t, team: team, root: t.TempDir(), uid: ownUID(t), clock: &sealClock{now: time.UnixMilli(sealMS)},
		units: map[string]*sealUnit{
			paths.ServerUnit(team): {active: "active", enabled: true, loaded: true},
			paths.TLSUnit(team):    {active: "active", enabled: true, loaded: true},
		},
		tail: Tail{
			Rooms: []TailEntry{{Room: "main", EventID: "$e1", TsMS: sealMS - 10}, {Room: "reports", EventID: "$e2", TsMS: sealMS - 20}},
			Epoch: restore.Epoch{RestoreEpoch: 2, Rev: 5},
		},
	}
	w.decl = sampleInput(t).Team
	for _, dir := range []string{paths.HomeserverPrivateState(team), paths.AdminState(team), paths.CtlState(team), paths.StatusTeamDir(team), paths.CarryDir, paths.AdminRunDir} {
		if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w.put(paths.TeamDeclaration(team), string(w.decl.Canonical()))
	w.put(paths.ServerNameFile(team), w.decl.ServerName)
	w.put(paths.ServerConfig(team), "server_name = \""+w.decl.ServerName+"\"\nport = 8448\n")
	w.put(paths.StampFile(team), `{"v":1,"team":"ops","config":"c","software":"s","tuwunel":"1.9.4","applied_ms":1}`)
	w.put(paths.UnitsRecord(team), unitsJSON(8448))
	w.put(paths.StatusFile(team), `{}`)
	w.put(paths.RestoreEpochFile(team), string(restore.EncodeEpochFloor(restore.EpochFloor{Epoch: 3, MinBackup: 8})))
	w.putRoster(func(*apply.RosterFile) {})
	return w
}

// sampleInput is the seal world's declaration of team ops.
func sampleInput(t *testing.T) declare.Input {
	t.Helper()
	in, err := declare.DecodeInput([]byte(`{"team":{"team":"ops","state":"present","port":8448,"description":"d",` +
		`"networks":["192.0.2.0/24"],"humans":["alice"],"trust":{"kind":"github",` +
		`"org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},"lxc_members":[],"address":"192.0.2.10"}`))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func ownUID(t *testing.T) uint32 {
	t.Helper()
	uid, err := strconv.ParseUint(strconv.Itoa(os.Getuid()), 10, 31)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(uid)
}

func unitsJSON(port int) string {
	set := func(keys ...string) string {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, `"`+k+`":"`+unitsDigestHex+`"`)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return `{"v":1,"team":"ops","restart":` + set("base_unit", "config", "exec_start") + `,"reload":` +
		set("ip_address_allow", "memory_high", "socket_bind_allow") + `,"tuwunel":"1.9.4","port":` + strconv.Itoa(port) + `,"listen":["192.0.2.10"]}`
}

// putRoster writes roster.json at seq 20, rev 5, restore epoch 2, changed by change.
func (w *sealWorld) putRoster(change func(*apply.RosterFile)) {
	w.t.Helper()
	r := apply.NewRosterFile(w.team)
	r.Seq, r.Rev, r.RestoreEpoch = 20, 5, 2
	change(&r)
	w.put(paths.RosterFile(w.team), string(r.Encode()))
}

func (w *sealWorld) put(path, content string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.root+path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(w.root+path, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *sealWorld) has(path string) bool {
	w.t.Helper()
	_, err := os.Lstat(w.root + path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		w.t.Fatal(err)
	}
	return err == nil
}

// record is sealed.json, its step "" when there is none.
func (w *sealWorld) record() SealRecord {
	w.t.Helper()
	if !w.has(paths.SealRecord(w.team)) {
		return SealRecord{}
	}
	raw, err := os.ReadFile(filepath.Clean(w.root + paths.SealRecord(w.team)))
	if err != nil {
		w.t.Fatal(err)
	}
	r, err := DecodeSealRecord(raw, w.team)
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

// effect records one call and dies at the kill-th.
func (w *sealWorld) effect(name string) {
	w.mu.Lock()
	w.n++
	n := w.n
	w.calls = append(w.calls, name)
	w.mu.Unlock()
	if n == w.kill {
		runtime.Goexit()
	}
}

// errInjected is the failure a world's fail-th call answers.
var errInjected = errors.New("injected failure")

// fault records one call as effect does, and answers errInjected when it is the fail-th: a
// failure the code sees and must clean up after, where a kill is one it never sees.
func (w *sealWorld) fault(name string) error {
	w.effect(name)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.n == w.fail {
		return errInjected
	}
	return nil
}

// mutations are the calls since mark that change something.
func (w *sealWorld) mutations(mark int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, c := range w.calls[mark:] {
		if !strings.Contains(c, "state ") && c != "ready" && c != "child tail" {
			out = append(out, c)
		}
	}
	return out
}

func (w *sealWorld) unit(name string) *sealUnit {
	u, ok := w.units[name]
	if !ok {
		u = &sealUnit{active: "inactive"}
		w.units[name] = u
	}
	return u
}

// sealUnits is the units manager as restore.Units.
type sealUnits struct{ w *sealWorld }

func (u sealUnits) State(_ context.Context, unit string) (restore.UnitState, error) {
	u.w.effect("units state " + unit)
	s := u.w.unit(unit)
	load := "not-found"
	if s.loaded {
		load = "loaded"
	}
	return restore.UnitState{Load: load, Active: s.active}, nil
}

func (u sealUnits) Stop(context.Context, string) error {
	u.w.t.Error("the seal stopped a unit through restore.Units")
	return nil
}

// sealSystemd is the units manager as host.Systemd.
type sealSystemd struct{ w *sealWorld }

func (s sealSystemd) State(_ context.Context, unit string) (host.UnitState, error) {
	s.w.effect("systemd state " + unit)
	u := s.w.unit(unit)
	return host.UnitState{Active: u.active, Enabled: u.enabled}, nil
}

func (s sealSystemd) Stop(_ context.Context, unit string) error {
	s.w.effect("stop " + unit)
	u := s.w.unit(unit)
	u.active = "inactive"
	if s.w.stuck && unit == paths.ServerUnit(s.w.team) {
		u.active = "activating"
	}
	return nil
}

func (s sealSystemd) Disable(_ context.Context, unit string) error {
	s.w.effect("disable " + unit)
	s.w.unit(unit).enabled = false
	return nil
}

func (s sealSystemd) DaemonReload(context.Context) error {
	if !s.w.reloads {
		return s.unexpected("daemon-reload")
	}
	s.w.effect("daemon-reload")
	return s.w.reloadErr
}

func (s sealSystemd) Start(context.Context, string) error      { return s.unexpected("start") }
func (s sealSystemd) Restart(context.Context, string) error    { return s.unexpected("restart") }
func (s sealSystemd) Enable(context.Context, string) error     { return s.unexpected("enable") }
func (s sealSystemd) CleanState(context.Context, string) error { return s.unexpected("clean") }

func (s sealSystemd) JournalTail(context.Context, string) (string, error) {
	return "", s.unexpected("journal")
}

func (s sealSystemd) unexpected(what string) error {
	s.w.t.Errorf("the seal called systemd's %s", what)
	return errors.New("unexpected")
}

type sealReady struct{ w *sealWorld }

func (r sealReady) ServerVersion(_ context.Context, port int) error {
	r.w.effect("ready")
	if r.w.silent || port != 8448 || r.w.unit(paths.ServerUnit(r.w.team)).active != "active" {
		return errors.New("connection refused")
	}
	return nil
}

// sealChild is the teamchat-ctl child's carry.tail.
type sealChild struct{ w *sealWorld }

func (c sealChild) Tail(_ context.Context, team paths.TeamID, teamFile []byte) (Tail, error) {
	c.w.effect("child tail")
	if team != c.w.team || string(teamFile) != string(c.w.decl.Canonical()) {
		return Tail{}, errors.New("a tail of another team")
	}
	return c.w.tail, c.w.tailErr
}

func (c sealChild) Place(context.Context, paths.TeamID, PlaceArgs) ([]string, error) {
	c.w.t.Error("the seal placed files")
	return nil, nil
}

func (c sealChild) Check(context.Context, paths.TeamID, CheckArgs) (Presence, error) {
	c.w.t.Error("the seal checked a tail")
	return Presence{}, nil
}

// sealFS is restore.OS with every write and removal an effect.
type sealFS struct {
	restore.OS

	w *sealWorld
}

func (f sealFS) WriteFile(path string, data []byte) error {
	name := filepath.Base(path)
	if name == "sealed.json" {
		r, err := DecodeSealRecord(data, f.w.team)
		if err != nil {
			f.w.t.Errorf("the seal wrote a sealed.json that does not decode: %v", err)
		}
		name += "=" + string(r.Step)
	}
	if err := f.w.fault("write " + name); err != nil {
		return err
	}
	return f.OS.WriteFile(path, data)
}

func (f sealFS) Remove(path string) error {
	if err := f.w.fault("remove " + filepath.Base(path)); err != nil {
		return err
	}
	return f.OS.Remove(path)
}

// sealStatus is host.Status: Remove deletes status.json.
type sealStatus struct{ w *sealWorld }

func (s sealStatus) Refresh(context.Context, paths.TeamID, bool, []string) error {
	s.w.t.Error("the seal refreshed the status record")
	return nil
}

func (s sealStatus) Remove(_ context.Context, team paths.TeamID) (string, error) {
	s.w.effect("status remove")
	err := os.Remove(s.w.root + paths.StatusFile(team))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return statusRemoved, err
}

func (w *sealWorld) deps() Deps {
	w.t.Helper()
	return Deps{
		Root: w.root, LockDir: w.root + paths.AdminRunDir, CtlUID: w.uid, StateOwnerUID: w.uid, Clock: w.clock,
		Software: mustDigest(w.t, sealSoftware), Units: sealUnits{w}, Systemd: sealSystemd{w}, Ready: sealReady{w},
		Child: sealChild{w}, FS: sealFS{OS: restore.OS{Root: w.root, StateOwnerUID: w.uid}, w: w}, Status: sealStatus{w},
	}
}

func (w *sealWorld) seal() ([]byte, error) { return Seal(context.Background(), w.deps(), w.team) }

// sealCalls are a fresh seal's calls in order: SC1's unit and server reads, SC2's tail, SC3's
// record, SC4's stop and disable and RS18's reads, the block, SC6 and SC7.
func sealCalls(team paths.TeamID) []string {
	tls, server := paths.TLSUnit(team), paths.ServerUnit(team)
	return []string{
		"units state " + paths.RestoreUnit(team), "units state " + paths.RestoreCtlUnit(team), "units state " + server, "ready",
		"child tail", "write sealed.json=sealing",
		"systemd state " + tls, "stop " + tls, "disable " + tls, "systemd state " + server, "stop " + server, "disable " + server,
		"systemd state " + tls, "systemd state " + server,
		"write restore.block", "remove stamp.json", "status remove", "write sealed.json=sealed",
	}
}

// sealedLines are a fresh seal's changed lines.
var sealedLines = []string{
	"seal recorded at step sealing", "tls unit stopped", "tls unit disabled", "unit stopped", "unit disabled",
	"restore.block written: no start", "stamp removed", statusRemoved, "seal recorded at step sealed",
}

// sealed asserts the sealed end state: the record at step sealed with SC5's facts, the units
// stopped and disabled, the block, and the team off the host's books.
func (w *sealWorld) sealed(at string) {
	w.t.Helper()
	r := w.record()
	if r.Step != StepSealed {
		w.t.Fatalf("%s: the record is at step %q", at, r.Step)
	}
	want := SealFacts{
		TimeMS: sealMS, ServerName: w.decl.ServerName, Port: mustPort(w.t, 8448), Tuwunel: mustVersion(w.t, "1.9.4"),
		Software: mustDigest(w.t, sealSoftware), Roster: RosterFacts{Seq: 20, Rev: 5, RestoreEpoch: 2},
		EpochFloor: EpochFloor{Epoch: 3, MinBackup: 8}, Tail: w.tail.Rooms,
	}
	if !sealEqual(r.Seal, want) {
		w.t.Errorf("%s: seal = %+v, want %+v", at, r.Seal, want)
	}
	for _, unit := range []string{paths.TLSUnit(w.team), paths.ServerUnit(w.team)} {
		if u := w.unit(unit); u.active != "inactive" || u.enabled {
			w.t.Errorf("%s: %s is %s, enabled %v", at, unit, u.active, u.enabled)
		}
	}
	for _, path := range []string{paths.RestoreBlock(w.team), paths.SealRecord(w.team)} {
		info, err := os.Lstat(w.root + path)
		if err != nil || info.Mode().Perm() != 0o600 {
			w.t.Errorf("%s: %s = %v, %v", at, path, info, err)
		}
	}
	if w.has(paths.StampFile(w.team)) || w.has(paths.StatusFile(w.team)) {
		w.t.Errorf("%s: the stamp or the status record is left", at)
	}
}

func sealEqual(a, b SealFacts) bool {
	return a.TimeMS == b.TimeMS && a.ServerName == b.ServerName && a.Port == b.Port && a.Tuwunel == b.Tuwunel &&
		a.Software == b.Software && a.Roster == b.Roster && a.EpochFloor == b.EpochFloor && slices.Equal(a.Tail, b.Tail)
}

func mustPort(t *testing.T, n int) paths.Port {
	t.Helper()
	p, err := paths.ParsePort(n)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustVersion(t *testing.T, s string) paths.ServerVersion {
	t.Helper()
	v, err := paths.ParseServerVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustDigest(t *testing.T, s string) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sealAnswer is the seal's answer: sealed.json's document with the changed lines.
type sealAnswer struct {
	doc     proto.Value
	changed []string
}

func answerOf(t *testing.T, raw []byte) sealAnswer {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}
	ch, found := v.Get("changed")
	if !found || ch.Kind() != proto.KindArray {
		t.Fatalf("the answer has no changed list: %s", raw)
	}
	a := sealAnswer{doc: v, changed: []string{}}
	for _, item := range ch.Items() {
		a.changed = append(a.changed, item.Str())
	}
	return a
}

// SC1-SC7: a seal stops and disables the units, blocks a start, takes the team off the host's
// books and records the facts read after the stop, in that order; the answer is sealed.json's
// document with what changed.
func TestSealSealsTheTeam(t *testing.T) {
	w := newSealWorld(t)
	out, err := w.seal()
	if err != nil {
		t.Fatal(err)
	}
	if want := sealCalls(w.team); !slices.Equal(w.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", w.calls, want)
	}
	w.sealed("the seal")
	if a := answerOf(t, out); !slices.Equal(a.changed, sealedLines) {
		t.Errorf("changed = %q, want %q", a.changed, sealedLines)
	}
	raw, err := os.ReadFile(filepath.Clean(w.root + paths.SealRecord(w.team)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), strings.TrimSuffix(string(raw), "}")+`,"changed":[`) {
		t.Errorf("the answer is not sealed.json's document:\n%s\n%s", out, raw)
	}
}

// SC5: an absent restore-epoch.json is {0, 0}.
func TestSealWithNoEpochFloorRecordsZero(t *testing.T) {
	w := newSealWorld(t)
	mustRemove(w, paths.RestoreEpochFile(w.team))
	if _, err := w.seal(); err != nil {
		t.Fatal(err)
	}
	if f := w.record().Seal.EpochFloor; f != (EpochFloor{}) {
		t.Errorf("epoch floor = %+v", f)
	}
}

// SC7: a seal of a sealed team answers the same document, changes nothing, and succeeds.
func TestSealOfASealedTeamChangesNothing(t *testing.T) {
	w := newSealWorld(t)
	first, err := w.seal()
	if err != nil {
		t.Fatal(err)
	}
	mark := len(w.calls)
	again, err := w.seal()
	if err != nil {
		t.Fatal(err)
	}
	if m := w.mutations(mark); len(m) != 0 {
		t.Errorf("the repeat changed %q", m)
	}
	a1, a2 := answerOf(t, first), answerOf(t, again)
	if len(a2.changed) != 0 {
		t.Errorf("the repeat's changed = %q", a2.changed)
	}
	for _, key := range []string{"sealed_ms", "server_name", "port", "tuwunel", "software", "roster", "epoch_floor", "tail", keyStep} {
		a, inFirst := a1.doc.Get(key)
		b, inAgain := a2.doc.Get(key)
		if !inFirst || !inAgain || string(a.Compact()) != string(b.Compact()) {
			t.Errorf("the repeat's %s = %s, the seal's %s", key, b.Compact(), a.Compact())
		}
	}
}

// SC1, SC2, SG3: each refusal is made before anything is written, and names the way on.
func TestSealRefusals(t *testing.T) {
	team := sampleTeam(t)
	for name, c := range map[string]struct {
		plant func(w *sealWorld)
		is    error
		names string
	}{
		"not applied": {func(w *sealWorld) { mustRemove(w, paths.StampFile(team)) }, restore.ErrRefused, "team apply"},
		"migration origin": {
			func(w *sealWorld) { w.put(paths.MigrationDir(team)+"/state.json", "{}") }, ErrRefused, "migration",
		},
		"a restore recorded": {func(w *sealWorld) { w.put(paths.RestoreRecord(team), "{}") }, restore.ErrPending, "--resume"},
		"a restore leftover": {
			func(w *sealWorld) { w.put(paths.HomeserverPrivateState(team)+"/db.previous/marker", "x") }, restore.ErrRefused, "db.previous",
		},
		"a loaded restore unit": {func(w *sealWorld) { w.unit(paths.RestoreUnit(team)).loaded = true }, restore.ErrRefused, paths.RestoreUnit(team)},
		"a drill":               {func(w *sealWorld) { w.put(paths.DrillRecord(team), "{}") }, ErrSealed, "--drill"},
		"an import":             {func(w *sealWorld) { w.put(paths.CarryImportRecord(team), "{}") }, ErrSealed, "--discard"},
		"a pending roster": {func(w *sealWorld) {
			w.putRoster(func(r *apply.RosterFile) {
				r.Pending = &apply.Pending{Op: apply.OpMemberRetire, Step: "rooms", SinceMS: 1, Handle: "web.1+desk.podman"}
			})
		}, ErrRefused, "member retire --team ops --handle web.1+desk.podman"},
		"a stopped server":       {func(w *sealWorld) { w.unit(paths.ServerUnit(team)).active = "inactive" }, ErrRefused, "team remove"},
		"a server not answering": {func(w *sealWorld) { w.silent = true }, ErrRefused, "team remove"},
		"a tail that disagrees":  {func(w *sealWorld) { w.tail.Epoch.Rev = 4 }, ErrRefused, "run team apply first"},
		"a seal record that does not decode": {
			func(w *sealWorld) { w.put(paths.SealRecord(team), `{"v":1,"team":"web","step":"sealing"}`) }, nil, "sealed.json",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newSealWorld(t)
			c.plant(w)
			before := w.has(paths.SealRecord(team))
			_, err := w.seal()
			if err == nil || !strings.Contains(err.Error(), c.names) {
				t.Fatalf("err = %v, want one naming %q", err, c.names)
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("err = %v, want %v", err, c.is)
			}
			if m := w.mutations(0); len(m) != 0 || w.has(paths.SealRecord(team)) != before {
				t.Errorf("the refused seal changed %q", m)
			}
		})
	}
}

func mustRemove(w *sealWorld, path string) {
	w.t.Helper()
	if err := os.Remove(w.root + path); err != nil {
		w.t.Fatal(err)
	}
}

// SC2, RS15: a tail the child could not read fails the seal with the child's error, before the
// record.
func TestSealTailFailureWritesNothing(t *testing.T) {
	w := newSealWorld(t)
	lost := errors.New("the homeserver was lost")
	w.tailErr = lost
	_, err := w.seal()
	if !errors.Is(err, lost) {
		t.Fatalf("err = %v", err)
	}
	if m := w.mutations(0); len(m) != 0 {
		t.Errorf("the seal changed %q", m)
	}
}

// SC4, RS18: a homeserver still activating after its stop fails the step: no block, the record
// left at sealing, and the next seal finishes once it stops.
func TestSealFailsOnAUnitThatDoesNotStop(t *testing.T) {
	w := newSealWorld(t)
	w.stuck = true
	_, err := w.seal()
	if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "activating") {
		t.Fatalf("err = %v, want a failure naming the state", err)
	}
	if w.record().Step != StepSealing || w.has(paths.RestoreBlock(w.team)) || !w.has(paths.StampFile(w.team)) {
		t.Fatalf("record %q, block %v, stamp %v", w.record().Step, w.has(paths.RestoreBlock(w.team)), w.has(paths.StampFile(w.team)))
	}
	w.stuck = false
	if _, err = w.seal(); err != nil {
		t.Fatal(err)
	}
	w.sealed("the second seal")
}

// SL1: the seal waits host.PlayWait for the team lock, then is busy.
func TestSealWaitsForTheLock(t *testing.T) {
	w := newSealWorld(t)
	held, err := teamlock.Take(context.Background(), w.root+paths.AdminRunDir, w.team, "apply", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.seal()
	if rerr := held.Release(); rerr != nil {
		t.Fatal(rerr)
	}
	if !errors.Is(err, teamlock.ErrBusy) || w.clock.slept < host.PlayWait {
		t.Fatalf("err = %v after waiting %v", err, w.clock.slept)
	}
	if len(w.calls) != 0 {
		t.Errorf("a seal that did not get the lock called %q", w.calls)
	}
}

// killSeal runs the seal until the kill and asserts it was killed.
func killSeal(t *testing.T, w *sealWorld) {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		returned := false
		defer func() { done <- returned }()
		if _, err := w.seal(); err != nil {
			t.Errorf("the run failed before its kill: %v", err)
		}
		returned = true
	}()
	if <-done {
		t.Fatal("the seal was not killed")
	}
}

// invariants are SM5's after a kill: nothing is written before the record, no block without
// the record and stopped units, no stamp removed without the block, no status record removed
// with the stamp left.
func (w *sealWorld) invariants(k int) {
	w.t.Helper()
	recorded, block := w.has(paths.SealRecord(w.team)), w.has(paths.RestoreBlock(w.team))
	stamp, status := w.has(paths.StampFile(w.team)), w.has(paths.StatusFile(w.team))
	if !recorded {
		for _, unit := range []string{paths.TLSUnit(w.team), paths.ServerUnit(w.team)} {
			if u := w.unit(unit); u.active != "active" || !u.enabled {
				w.t.Errorf("kill %d: %s was stopped or disabled with no record", k, unit)
			}
		}
		if block || !stamp || !status {
			w.t.Errorf("kill %d: changes with no record: block %v, stamp %v, status %v", k, block, stamp, status)
		}
	}
	if block && w.unit(paths.ServerUnit(w.team)).active == "active" {
		w.t.Errorf("kill %d: the block with the homeserver active", k)
	}
	if !stamp && !block {
		w.t.Errorf("kill %d: the stamp removed with no block", k)
	}
	if !status && stamp {
		w.t.Errorf("kill %d: the status record removed with the stamp left", k)
	}
	if w.record().Step == StepSealed {
		w.t.Errorf("kill %d: sealed before the last call", k)
	}
}

// SM5: a failure answered by any write or removal of the seal is its failure, never a
// refusal; the invariants hold as after a kill, and the next seal ends sealed.
func TestSealFailureAtEveryWrite(t *testing.T) {
	for k, call := range sealCalls(sampleTeam(t)) {
		if !strings.HasPrefix(call, "write ") && !strings.HasPrefix(call, "remove ") {
			continue
		}
		at := "failure " + strconv.Itoa(k+1) + " (" + call + ")"
		w := newSealWorld(t)
		w.fail = k + 1
		if _, err := w.seal(); !errors.Is(err, errInjected) || errors.Is(err, ErrRefused) {
			t.Fatalf("%s: err = %v", at, err)
		}
		if w.has(paths.SealRecord(w.team)) {
			w.invariants(k + 1)
		}
		w.fail = 0
		if _, err := w.seal(); err != nil {
			t.Fatalf("%s: the next seal: %v", at, err)
		}
		w.sealed(at)
	}
}

// SM5: a kill at every call of the seal leaves the invariants, and the next seal ends sealed;
// one more seal changes nothing.
func TestSealKillAtEveryCall(t *testing.T) {
	calls := sealCalls(sampleTeam(t))
	for k := 1; k <= len(calls); k++ {
		t.Run(strconv.Itoa(k)+" "+calls[k-1], func(t *testing.T) {
			t.Parallel()
			w := newSealWorld(t)
			w.kill = k
			killSeal(t, w)
			w.invariants(k)
			w.kill = 0
			if _, err := w.seal(); err != nil {
				t.Fatalf("kill %d: the next seal: %v", k, err)
			}
			w.sealed("kill " + strconv.Itoa(k))
			mark := len(w.calls)
			if _, err := w.seal(); err != nil {
				t.Fatal(err)
			}
			if m := w.mutations(mark); len(m) != 0 {
				t.Errorf("kill %d: the repeat changed %q", k, m)
			}
		})
	}
}
