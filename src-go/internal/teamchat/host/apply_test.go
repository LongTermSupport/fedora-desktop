package host

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ops is the sample team's id.
func ops(t *testing.T) paths.TeamID {
	t.Helper()
	return mustTeam(t, "ops")
}

// approved is the sample input, approved in w.
func approved(w *world, doc map[string]any) declare.Input {
	in := input(w.t, doc)
	w.approve(in)
	return in
}

// applied is a world in which the sample team has been applied once; w.cur is its input.
func applied(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	if _, err := w.apply(approved(w, teamDoc())); err != nil {
		t.Fatal(err)
	}
	return w
}

func stepOf(t *testing.T, err error) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("got %v, want a host Error", err)
	}
	return e
}

func before(t *testing.T, log []string, a, b string) {
	t.Helper()
	ia, ib := slices.Index(log, a), slices.Index(log, b)
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("%s must come before %s in %q", a, b, log)
	}
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func lockIsFree(t *testing.T, w *world) {
	t.Helper()
	l, err := TakeLock(t.Context(), w.at(paths.AdminRunDir), ops(t), "check", 0, w.clock)
	if err != nil {
		t.Errorf("the lock was not released: %v", err)
		return
	}
	releaseOrFail(t, l)
}

// HS3, HS6, HS9, HS11: a first apply makes the team's files, its firewall rule, its drop-in,
// starts and enables the unit, waits for it, bootstraps, and writes the records, stamp last.
func TestFirstApply(t *testing.T) {
	w := newWorld(t)
	w.boot = func(Bootstrap) []string { return []string{"human alice created"} }
	in := approved(w, teamDoc())
	lines, err := w.apply(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"files created", "firewall added public 192.0.2.0/24 tcp:8448", "drop-in written",
		"unit started", "unit enabled", "human alice created", "team.json written", "units.json written",
		"stamp written",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q\nwant %q", lines, want)
	}
	team := ops(t)
	if got := w.read(paths.ServerNameFile(team)); got != "ops.teamchat.internal" {
		t.Errorf("server-name %q", got)
	}
	key := strings.Repeat("ab", 64)
	if got := regKeyOf([]byte(w.read(paths.ServerConfig(team)))); got != key {
		t.Errorf("config key %q", got)
	}
	firstApplyModes(t, w, team)
	if got := w.read(paths.TeamDeclaration(team)); got != string(in.Team.Canonical()) {
		t.Errorf("team.json %s", got)
	}
	st, err := decodeStamp([]byte(w.read(paths.StampFile(team))), team)
	if err != nil {
		t.Fatal(err)
	}
	if want := (stamp{config: digest(in.Team.Canonical()), software: "d1g3st", tuwunel: "1.9.4", appliedMS: w.clock.now.UnixMilli()}); st != want {
		t.Errorf("stamp %+v, want %+v", st, want)
	}
	if len(w.boots) != 1 || w.boots[0].RegKey != key || string(w.boots[0].TeamFile) != string(in.Team.Canonical()) || w.boots[0].Input.ID() != team {
		t.Errorf("bootstrap given %+v", w.boots)
	}
	if !slices.Equal(w.status.refreshed, []bool{true}) {
		t.Errorf("status refreshed %v", w.status.refreshed)
	}
	if w.backups != 0 {
		t.Error("a team with no database was backed up")
	}
	for _, pair := range [][2]string{{"add", "daemon-reload"}, {"daemon-reload", "start"}, {"start", "ready"}, {"ready", "bootstrap"}, {"bootstrap", "status.refresh"}} {
		before(t, w.log, pair[0], pair[1])
	}
	lockIsFree(t, w)
}

// firstApplyModes holds each file and directory of a first apply to its mode.
func firstApplyModes(t *testing.T, w *world, team paths.TeamID) {
	t.Helper()
	for p, m := range map[string]os.FileMode{
		paths.AdminState(team): 0o700, paths.CtlState(team): 0o700, paths.ServerConfig(team): 0o600,
		paths.ServerNameFile(team): 0o600, paths.ServerDropInFile(team): 0o644, paths.TeamDeclaration(team): 0o600,
		paths.UnitsRecord(team): 0o600, paths.FirewallRecord(team): 0o600, paths.StampFile(team): 0o644,
		filepath.Dir(paths.ServerDropInFile(team)): 0o755, paths.StatusTeamDir(team): 0o755,
	} {
		if got := mode(t, w.at(p)); got != m {
			t.Errorf("%s: mode %v, want %v", p, got, m)
		}
	}
}

// HS12: a second apply with nothing changed reports nothing and changes nothing.
func TestSecondApplyChangesNothing(t *testing.T) {
	w := applied(t)
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("lines %q", lines)
	}
	for _, call := range []string{"add", "remove", "daemon-reload", "start", "restart", "enable", "backup"} {
		if w.called(call) {
			t.Errorf("%s was called: %q", call, w.log)
		}
	}
	if !slices.Equal(w.status.refreshed, []bool{true, false}) {
		t.Errorf("status refreshed %v", w.status.refreshed)
	}
}

// HS6: a unit found stopped with nothing to change is started again.
func TestStoppedUnitIsStarted(t *testing.T) {
	w := applied(t)
	w.sd.unit(paths.ServerUnit(ops(t))).Active = "inactive"
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"unit started"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q", lines)
	}
}

// HS3: a server_name other than the one fixed at the first apply is refused, even when that
// first apply failed after fixing it.
func TestServerNameIsFixedByTheFirstApply(t *testing.T) {
	w := newWorld(t)
	w.faults["ready"] = errFault
	in := approved(w, teamDoc())
	if _, err := w.apply(in); stepOf(t, err).Step != StepReady {
		t.Fatalf("got %v", err)
	}
	delete(w.faults, "ready")
	doc := teamDoc()
	docTeam(doc)["server_name"] = "other.teamchat.internal"
	renamed := approved(w, doc)
	config := w.read(paths.ServerConfig(ops(t)))
	_, err := w.apply(renamed)
	if e := stepOf(t, err); e.Step != StepRefusals || !e.Drift || !strings.Contains(err.Error(), "server_name") {
		t.Fatalf("got %v", err)
	}
	if w.called("add") || w.called("start") || w.called("restart") || w.read(paths.ServerConfig(ops(t))) != config {
		t.Errorf("a refused apply changed something: %q", w.log)
	}
	lines, err := w.apply(in)
	if err != nil {
		t.Fatalf("the fixed name is still applied: %v", err)
	}
	if slices.Contains(lines, "files created") {
		t.Errorf("a later apply made the first apply's files again: %q", lines)
	}
	if !slices.Equal(w.status.refreshed, []bool{true}) {
		t.Errorf("the first apply to finish is not the status record's first: %v", w.status.refreshed)
	}
}

// HS3: the refusals before any change, each drift (exit 70).
func TestRefusalsBeforeAnyChange(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(*world) declare.Input
		says  string
	}{
		"not approved": {func(w *world) declare.Input { return input(w.t, teamDoc()) }, "approve"},
		"approval changed": {func(w *world) declare.Input {
			approved(w, teamDoc())
			doc := teamDoc()
			docTeam(doc)["humans"] = []any{"alice", "mallory"}
			return input(w.t, doc)
		}, "mallory"},
		"listen on no interface": {func(w *world) declare.Input {
			doc := teamDoc()
			docTeam(doc)["listen"] = []any{"192.0.2.99"}
			return approved(w, doc)
		}, "192.0.2.99 is on no interface"},
		"listen on two interfaces": {func(w *world) declare.Input {
			w.net.ifaces = append(w.net.ifaces, Interface{Name: "eth1", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}})
			return approved(w, teamDoc())
		}, "192.0.2.10 is on more than one interface"},
		"wildcard listen": {func(w *world) declare.Input {
			in := input(w.t, teamDoc())
			in.Team.Listen = []string{"0.0.0.0"}
			w.approve(in)
			return in
		}, "wildcard"},
		"port another team records": {func(w *world) declare.Input {
			dev := mustTeam(w.t, "dev")
			if err := os.Mkdir(w.at(paths.AdminState(dev)), 0o700); err != nil {
				w.t.Fatal(err)
			}
			w.write(paths.UnitsRecord(dev), string(sampleUnits(w.t).encode(dev)))
			return approved(w, teamDoc())
		}, "team dev"},
		"port another program listens on": {func(w *world) declare.Input {
			w.net.extra = []Listener{listener("0.0.0.0", 8448, "other.service")}
			return approved(w, teamDoc())
		}, "other.service"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			in := c.setup(w)
			lines, err := w.apply(in)
			e := stepOf(t, err)
			if e.Step != StepRefusals || !e.Drift || !strings.Contains(err.Error(), c.says) {
				t.Errorf("got %v (drift %v), want a refusal saying %q", err, e.Drift, c.says)
			}
			if len(lines) != 0 || w.exists(paths.ServerNameFile(ops(t))) || w.called("zone") || w.called("start") {
				t.Errorf("a refusal changed something: %q %q", lines, w.log)
			}
		})
	}
}

// HS3: other teams' records that are not teams, or are unreadable, are not ports.
func TestPortCheckReadsOnlyTeams(t *testing.T) {
	w := newWorld(t)
	if err := os.Mkdir(w.at(paths.AdminLXCDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(w.at(paths.AdminState(mustTeam(t, "dev"))), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.apply(approved(w, teamDoc())); err != nil {
		t.Fatalf("a team with no units.json or a directory that is no team blocked the apply: %v", err)
	}
	w.write(paths.UnitsRecord(mustTeam(t, "dev")), "{")
	if _, err := w.apply(w.cur); stepOf(t, err).Step != StepRefusals || stepOf(t, err).Drift {
		t.Errorf("an unreadable units.json of another team: %v", err)
	}
}

// HS13: each step's failure fails the verb with the step named, is not drift, and stops every
// step after it.
func TestEachFailureStopsWhatFollows(t *testing.T) {
	for fault, step := range map[string]string{
		"approvals.load": StepRefusals, "interfaces": StepRefusals, "listeners": StepRefusals,
		"zone": StepFirewall, "query": StepFirewall, "add": StepFirewall,
		"daemon-reload": StepUnit, "start": StepUnit, "state": StepUnit, "enable": StepUnit,
		"ready": StepReady, "journal": StepReady, "bootstrap": StepBootstrap, "status.refresh": StepRecords,
	} {
		t.Run(fault, func(t *testing.T) {
			w := newWorld(t)
			w.faults[fault] = errFault
			if fault == "journal" {
				w.sd.startTo = "failed"
			}
			_, err := w.apply(approved(w, teamDoc()))
			e := stepOf(t, err)
			if e.Step != step || e.Drift || !strings.Contains(err.Error(), "team apply, step "+step+": ") {
				t.Fatalf("got %v, want step %s", err, step)
			}
			stopped := map[string]bool{
				StepRefusals: !w.called("zone"), StepFirewall: !w.called("start"), StepUnit: !w.called("ready"),
				StepReady: !w.called("bootstrap"), StepBootstrap: !w.exists(paths.TeamDeclaration(ops(t))),
				StepRecords: w.exists(paths.StampFile(ops(t))),
			}
			if !stopped[step] {
				t.Errorf("a failure at %s did not stop what follows: %q", step, w.log)
			}
			if step != StepRecords && w.exists(paths.StampFile(ops(t))) {
				t.Error("a stamp was written by a failed apply")
			}
			lockIsFree(t, w)
		})
	}
}

// HS13: what Bootstrap answered before it failed is kept, in order, and its error is wrapped.
func TestBootstrapFailureKeepsItsLines(t *testing.T) {
	w := newWorld(t)
	w.boot = func(Bootstrap) []string { return []string{"human alice created"} }
	w.faults["bootstrap"] = errFault
	lines, err := w.apply(approved(w, teamDoc()))
	if !errors.Is(err, errFault) || lines[len(lines)-1] != "human alice created" {
		t.Errorf("lines %q, err %v", lines, err)
	}
}

// HS2: a team lock held by another verb is busy, with the holder named.
func TestApplyBusy(t *testing.T) {
	w := newWorld(t)
	held, err := TakeLock(t.Context(), w.at(paths.AdminRunDir), ops(t), "backup", 0, w.clock)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, held)
	d := w.deps()
	d.LockWait = 0
	_, err = Apply(t.Context(), d, approved(w, teamDoc()))
	busy, ok := errors.AsType[*BusyError](err)
	if !ok || !strings.HasPrefix(busy.Holder, "backup ops pid=") || stepOf(t, err).Step != StepLock {
		t.Errorf("got %v", err)
	}
}

// HS10: the bound address set must be loopback and listen exactly.
func TestBoundAddressesAreAsserted(t *testing.T) {
	w := newWorld(t)
	w.net.bound = []Listener{listener("127.0.0.1", 8448, paths.ServerUnit(ops(t)))}
	_, err := w.apply(approved(w, teamDoc()))
	if e := stepOf(t, err); e.Step != StepAddresses || e.Drift || !strings.Contains(err.Error(), "192.0.2.10") {
		t.Fatalf("got %v", err)
	}
	if w.called("bootstrap") {
		t.Error("bootstrap ran over the wrong addresses")
	}
}

// HS9: readiness waits for an activating unit and for the probe, within 60 s.
func TestReadinessWaits(t *testing.T) {
	w := newWorld(t)
	w.sd.activating, w.ready.fails = 2, 3
	if _, err := w.apply(approved(w, teamDoc())); err != nil {
		t.Fatal(err)
	}
	if got := w.clock.now.Sub(t0); got != 5*readyPoll {
		t.Errorf("waited %v, want %v", got, 5*readyPoll)
	}
}

// HS9: a unit that never answers fails after 60 s; one that fails fails at once, with the
// journal tail only as explanation.
func TestReadinessFailures(t *testing.T) {
	w := newWorld(t)
	w.ready.fails = 1000
	_, err := w.apply(approved(w, teamDoc()))
	if e := stepOf(t, err); e.Step != StepReady || !strings.Contains(err.Error(), "1m0s") {
		t.Fatalf("got %v", err)
	}
	if got := w.clock.now.Sub(t0); got < readyWait || got > readyWait+readyPoll {
		t.Errorf("waited %v", got)
	}

	w = newWorld(t)
	w.sd.startTo, w.sd.journal = "failed", "tuwunel: bad config"
	_, err = w.apply(approved(w, teamDoc()))
	if e := stepOf(t, err); e.Step != StepReady || !strings.Contains(err.Error(), "is failed") || !strings.Contains(err.Error(), "tuwunel: bad config") {
		t.Fatalf("got %v", err)
	}
	if w.called("ready") || w.clock.now != t0 {
		t.Error("a failed unit was probed or waited for")
	}
}

func makeDatabase(t *testing.T, w *world) {
	t.Helper()
	if err := os.MkdirAll(w.at(paths.HomeserverPrivateDatabase(ops(t))), 0o700); err != nil {
		t.Fatal(err)
	}
}

// HS5, HS8: a changed restart set backs up a team with a database, then restarts it.
func TestRestartBacksUpFirst(t *testing.T) {
	w := applied(t)
	makeDatabase(t, w)
	w.write(paths.ServerUnitTemplateFile, "[Unit]\nDescription=a new base unit\n")
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"backup taken", "unit restarted", "units.json written"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	before(t, w.log, "backup", "restart")
	if w.called("daemon-reload") {
		t.Error("a base unit change alone reloaded")
	}
}

// HS5: a changed restart set with no database restarts without a backup; a new version writes
// the drop-in and the config stays.
func TestNewVersionRestarts(t *testing.T) {
	w := applied(t)
	w.version = "1.9.5"
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"drop-in written", "unit restarted", "units.json written", "stamp written"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if !strings.Contains(w.read(paths.ServerDropInFile(ops(t))), "tuwunel-1.9.5") || w.backups != 0 {
		t.Error("the new version is not in the drop-in, or a team with no database was backed up")
	}
}

// HS6: a hand-edited config is rewritten, and restarts nothing when the running one is the
// rendered one.
func TestEditedConfigIsRewritten(t *testing.T) {
	w := applied(t)
	config := w.read(paths.ServerConfig(ops(t)))
	w.write(paths.ServerConfig(ops(t)), config+"log = \"debug\"\n")
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"config written"}; !slices.Equal(lines, want) || w.read(paths.ServerConfig(ops(t))) != config {
		t.Errorf("lines %q", lines)
	}
}

// HS8: a failed backup stops the apply before anything is rewritten or restarted.
func TestFailedBackupStopsTheApply(t *testing.T) {
	w := applied(t)
	makeDatabase(t, w)
	w.version = "1.9.5"
	w.faults["backup"] = errFault
	dropIn := w.read(paths.ServerDropInFile(ops(t)))
	_, err := w.apply(w.cur)
	if stepOf(t, err).Step != StepBackup {
		t.Fatalf("got %v", err)
	}
	if w.called("restart") || w.read(paths.ServerDropInFile(ops(t))) != dropIn {
		t.Errorf("a failed backup let the apply go on: %q", w.log)
	}
}

// HS5, HS7: a changed allow_from is a firewall change and a reload, never a restart.
func TestReloadSetOnly(t *testing.T) {
	w := applied(t)
	doc := teamDoc()
	docTeam(doc)["allow_from"] = []any{"203.0.113.0/24"}
	in := approved(w, doc)
	makeDatabase(t, w)
	lines, err := w.apply(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"firewall added public 203.0.113.0/24 tcp:8448", "firewall removed public 192.0.2.0/24 tcp:8448",
		"drop-in written", "team.json written", "units.json written", "stamp written",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q\nwant %q", lines, want)
	}
	if w.called("restart") || w.called("start") || w.backups != 0 || !w.called("daemon-reload") {
		t.Errorf("a reload-set change: %q", w.log)
	}
}

// HS5: a description change is a write of team.json and the stamp alone.
func TestWriteWithoutRestart(t *testing.T) {
	w := applied(t)
	doc := teamDoc()
	docTeam(doc)["description"] = "The team that runs other things."
	lines, err := w.apply(approved(w, doc))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"team.json written", "stamp written"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if w.called("daemon-reload") || w.called("restart") {
		t.Errorf("a description change touched the unit: %q", w.log)
	}
}

// HS7: firewall.json holds the union while rules are added and removed, adds come before
// removes, each in the permanent and the runtime configuration, then the new set alone.
func TestFirewallUnionOrder(t *testing.T) {
	w := applied(t)
	team := ops(t)
	union := `[{"zone":"public","source":"192.0.2.0/24","port":8448},{"zone":"public","source":"203.0.113.0/24","port":8448}]`
	check := func(Rule) {
		if got := w.read(paths.FirewallRecord(team)); !strings.Contains(got, union) {
			t.Errorf("firewall.json during the change: %s", got)
		}
	}
	w.fw.onAdd, w.fw.onDrop = check, check
	doc := teamDoc()
	docTeam(doc)["allow_from"] = []any{"203.0.113.0/24"}
	if _, err := w.apply(approved(w, doc)); err != nil {
		t.Fatal(err)
	}
	if got := w.read(paths.FirewallRecord(team)); !strings.Contains(got, `"rules":[{"zone":"public","source":"203.0.113.0/24","port":8448}]`) {
		t.Errorf("firewall.json after: %s", got)
	}
	if slices.Index(w.log, "add") > slices.Index(w.log, "remove") {
		t.Errorf("a remove came before an add: %q", w.log)
	}
	for _, permanent := range []bool{true, false} {
		if !w.fw.present[permanent]["public 203.0.113.0/24 tcp:8448"] || w.fw.present[permanent]["public 192.0.2.0/24 tcp:8448"] {
			t.Errorf("permanent %v: %v", permanent, w.fw.present[permanent])
		}
	}
}

// HS7: a rule lost from the runtime configuration alone is added back there.
func TestFirewallConvergesEachConfiguration(t *testing.T) {
	w := applied(t)
	delete(w.fw.present[false], "public 192.0.2.0/24 tcp:8448")
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"firewall added public 192.0.2.0/24 tcp:8448"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q", lines)
	}
	adds := 0
	for _, call := range w.log {
		if call == "add" {
			adds++
		}
	}
	if adds != 1 {
		t.Errorf("%d adds", adds)
	}
}

// HS7: a removal that fails stops the apply with firewall.json still holding the union.
func TestFirewallRemoveFailureKeepsTheUnion(t *testing.T) {
	w := applied(t)
	w.faults["remove"] = errFault
	doc := teamDoc()
	docTeam(doc)["allow_from"] = []any{"203.0.113.0/24"}
	if _, err := w.apply(approved(w, doc)); stepOf(t, err).Step != StepFirewall {
		t.Fatalf("got %v", err)
	}
	if got := w.read(paths.FirewallRecord(ops(t))); !strings.Contains(got, "192.0.2.0/24") || !strings.Contains(got, "203.0.113.0/24") {
		t.Errorf("firewall.json %s", got)
	}
}

// HS4: a team with an LXC member admits the bridge; the bridge's failure is the render's.
func TestLXCMemberAdmitsTheBridge(t *testing.T) {
	doc := teamDoc()
	doc["lxc_members"] = []any{map[string]any{"team": "ops", "container": "c1", "user": "agent", "checkout": "/srv/checkouts/thing"}}
	w := newWorld(t)
	lines, err := w.apply(approved(w, doc))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "firewall added public 198.51.100.0/24 tcp:8448") || !strings.Contains(w.read(paths.ServerDropInFile(ops(t))), " 198.51.100.0/24\n") {
		t.Errorf("lines %q", lines)
	}
	w = newWorld(t)
	w.faults["bridge"] = errFault
	if _, err := w.apply(approved(w, doc)); stepOf(t, err).Step != StepRender {
		t.Errorf("got %v", err)
	}
}

// removed is what `team remove` without --purge leaves: the unit stopped and disabled, no
// drop-in, firewall rules or stamp; the records and the database stay.
func removed(t *testing.T, w *world) {
	t.Helper()
	team := ops(t)
	for _, p := range []string{paths.ServerDropInFile(team), paths.FirewallRecord(team), paths.StampFile(team)} {
		if err := os.Remove(w.at(p)); err != nil {
			t.Fatal(err)
		}
	}
	*w.sd.unit(paths.ServerUnit(team)) = UnitState{Active: "inactive"}
	w.fw.present = map[bool]map[string]bool{true: {}, false: {}}
}

// HS8: a team removed without --purge and brought back on a new version is started on the
// version that wrote its database, backed up, and restarted on the new one.
func TestBroughtBackStartsTheOldVersionForTheBackup(t *testing.T) {
	w := applied(t)
	team := ops(t)
	makeDatabase(t, w)
	removed(t, w)
	w.version = "1.9.5"
	w.onBackup = func() {
		if got := w.read(paths.ServerDropInFile(team)); !strings.Contains(got, "tuwunel-1.9.4") || strings.Contains(got, "After=") {
			t.Errorf("the drop-in at the backup:\n%s", got)
		}
	}
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"firewall added public 192.0.2.0/24 tcp:8448", "drop-in written for the backup, tuwunel 1.9.4",
		"unit started for the backup", "backup taken", "drop-in written", "unit restarted", "unit enabled",
		"units.json written", "stamp written",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q\nwant %q", lines, want)
	}
	if !strings.Contains(w.read(paths.ServerDropInFile(team)), "tuwunel-1.9.5") || w.backups != 1 {
		t.Error("the drop-in does not name the new version, or no backup")
	}
}

// HS8: an upgrade whose restart failed leaves the new drop-in and the old units.json; the next
// apply backs the database up on the old version, not the new one.
func TestAFailedUpgradeIsBackedUpOnTheOldVersion(t *testing.T) {
	w := applied(t)
	team := ops(t)
	makeDatabase(t, w)
	w.version = "1.9.5"
	w.faults["restart"] = errFault
	if _, err := w.apply(w.cur); stepOf(t, err).Step != StepUnit {
		t.Fatalf("got %v", err)
	}
	delete(w.faults, "restart")
	*w.sd.unit(paths.ServerUnit(team)) = UnitState{Active: "inactive"}
	w.onBackup = func() {
		if got := w.read(paths.ServerDropInFile(team)); !strings.Contains(got, "tuwunel-1.9.4") {
			t.Errorf("the drop-in at the backup:\n%s", got)
		}
	}
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "drop-in written for the backup, tuwunel 1.9.4") || w.backups != 2 {
		t.Errorf("lines %q, backups %d", lines, w.backups)
	}
	if !strings.Contains(w.read(paths.ServerDropInFile(team)), "tuwunel-1.9.5") {
		t.Error("the drop-in does not name the new version")
	}
}

// HS8: a database with no record of the version that wrote it is not started blind.
func TestDatabaseWithNoVersionRecordIsRefused(t *testing.T) {
	w := applied(t)
	makeDatabase(t, w)
	removed(t, w)
	if err := os.Remove(w.at(paths.UnitsRecord(ops(t)))); err != nil {
		t.Fatal(err)
	}
	_, err := w.apply(w.cur)
	if e := stepOf(t, err); e.Step != StepBackup || !strings.Contains(err.Error(), "units.json") {
		t.Errorf("got %v", err)
	}
	if w.called("start") {
		t.Error("started blind")
	}
}

// HS5: the five-way answer.
func TestDecide(t *testing.T) {
	base, err := render(spec(t, input(t, teamDoc())))
	if err != nil {
		t.Fatal(err)
	}
	units := &unitsRecord{digests: base.digests}
	other := func(change func(*renderSpec)) rendered {
		s := spec(t, input(t, teamDoc()))
		change(&s)
		r, err := render(s)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for name, c := range map[string]struct {
		dropIn   bool
		units    *unitsRecord
		r        rendered
		teamFile []byte
		want     answer
	}{
		"no drop-in":     {false, units, base, base.teamFile, answerFirst},
		"no units.json":  {true, nil, base, base.teamFile, answerRestart},
		"restart set":    {true, units, other(func(s *renderSpec) { s.baseUnit = []byte("x") }), base.teamFile, answerRestart},
		"reload set":     {true, units, other(func(s *renderSpec) { s.pin.MemoryHigh = "1G" }), base.teamFile, answerReload},
		"team.json only": {true, units, base, []byte("{}"), answerWrite},
		"nothing":        {true, units, base, base.teamFile, answerNothing},
	} {
		if got := decide(c.dropIn, c.units, c.r, c.teamFile); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
