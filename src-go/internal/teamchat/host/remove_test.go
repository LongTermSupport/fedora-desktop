package host

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// remove runs Remove over the sample team with a fresh log.
func remove(t *testing.T, w *world, purge bool) ([]string, error) {
	t.Helper()
	w.log = nil
	return Remove(t.Context(), w.deps(), ops(t), purge)
}

// purgeable is an applied world with a database and an approval file, as a host has them.
func purgeable(t *testing.T) *world {
	t.Helper()
	w := applied(t)
	makeDatabase(t, w)
	w.write(paths.ApprovedFile(ops(t)), "{}")
	return w
}

// HS14: a team that is not installed is a success that reports nothing and calls nothing.
func TestRemoveNotInstalled(t *testing.T) {
	for _, purge := range []bool{false, true} {
		w := newWorld(t)
		lines, err := remove(t, w, purge)
		if err != nil || lines != nil {
			t.Errorf("purge %v: lines %q, err %v", purge, lines, err)
		}
		if len(w.log) != 0 {
			t.Errorf("purge %v: calls %q", purge, w.log)
		}
		lockIsFree(t, w)
	}
}

// HS14: remove stops and disables the unit, removes the recorded rules in both configurations,
// the drop-in and the stamp, and hands the status record to its writer; the team's data stays.
func TestRemove(t *testing.T) {
	w := applied(t)
	w.status.removeLine = "status record removed"
	team := ops(t)
	lines, err := remove(t, w, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"unit stopped", "unit disabled", "firewall removed public 192.0.2.0/24 tcp:8448", "drop-in removed",
		"stamp removed", "status record removed",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q\nwant %q", lines, want)
	}
	for _, p := range []string{
		paths.ServerDropInFile(team), filepath.Dir(paths.ServerDropInFile(team)), paths.FirewallRecord(team), paths.StampFile(team),
	} {
		if w.exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	for _, p := range []string{
		paths.AdminState(team), paths.CtlState(team), paths.ServerConfig(team), paths.ServerNameFile(team),
		paths.UnitsRecord(team), paths.TeamDeclaration(team),
	} {
		if !w.exists(p) {
			t.Errorf("%s was removed without --purge", p)
		}
	}
	if len(w.fw.present[true])+len(w.fw.present[false]) != 0 {
		t.Errorf("rules left %v", w.fw.present)
	}
	for _, pair := range [][2]string{{"stop", "remove"}, {"remove", "daemon-reload"}, {"daemon-reload", "status.remove"}} {
		before(t, w.log, pair[0], pair[1])
	}
	if w.called("clean") {
		t.Error("the homeserver's state was cleaned without --purge")
	}
	lockIsFree(t, w)
	if lines, err := remove(t, w, false); err != nil || lines != nil {
		t.Errorf("a second remove: lines %q, err %v", lines, err)
	}
}

// HS14: a unit already stopped and disabled is left alone.
func TestRemoveStoppedUnit(t *testing.T) {
	w := applied(t)
	*w.sd.unit(paths.ServerUnit(ops(t))) = UnitState{Active: "inactive"}
	lines, err := remove(t, w, false)
	if err != nil || slices.Contains(lines, "unit stopped") || slices.Contains(lines, "unit disabled") {
		t.Errorf("lines %q, err %v", lines, err)
	}
	if w.called("stop") || w.called("disable") {
		t.Errorf("calls %q", w.log)
	}
}

// HS14: only the recorded rules are removed; a rule recorded with an old port is removed too,
// and a rule the record does not name is not touched.
func TestRemoveOnlyRecordedRules(t *testing.T) {
	w := applied(t)
	team := ops(t)
	old := Rule{Zone: "public", Source: netip.MustParsePrefix("203.0.113.0/24"), Port: 8008}
	other := Rule{Zone: "public", Source: netip.MustParsePrefix("198.51.100.0/24"), Port: 9000}
	recorded := []Rule{{Zone: "public", Source: netip.MustParsePrefix("192.0.2.0/24"), Port: 8448}, old}
	w.write(paths.FirewallRecord(team), string(encodeFirewall(team, recorded)))
	w.fw.present[true][old.String()] = true
	w.fw.present[true][other.String()], w.fw.present[false][other.String()] = true, true
	lines, err := remove(t, w, false)
	if err != nil || !slices.Contains(lines, "firewall removed "+old.String()) {
		t.Errorf("lines %q, err %v", lines, err)
	}
	if !w.fw.present[true][other.String()] || !w.fw.present[false][other.String()] || len(w.fw.present[true]) != 1 {
		t.Errorf("rules left %v", w.fw.present)
	}
}

// HS14: another file in the drop-in directory is the owner's and stays, with its directory.
func TestRemoveKeepsAnotherDropIn(t *testing.T) {
	w := applied(t)
	dir := filepath.Dir(paths.ServerDropInFile(ops(t)))
	w.write(dir+"/owner.conf", "[Service]\n")
	if _, err := remove(t, w, false); err != nil {
		t.Fatal(err)
	}
	if !w.exists(dir+"/owner.conf") || w.exists(paths.ServerDropInFile(ops(t))) {
		t.Error("the owner's drop-in was removed, or the team's was not")
	}
}

// HS14: --purge also cleans the homeserver's state and removes ctl/<team>/, the approval and
// root/<team>/, the last of them last; the status directory stays for the status record's writer.
func TestRemovePurge(t *testing.T) {
	w := purgeable(t)
	team := ops(t)
	lines, err := remove(t, w, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"homeserver state cleaned", "ctl state removed", "approval removed", "root state removed"}
	if len(lines) < len(want) || !slices.Equal(lines[len(lines)-len(want):], want) {
		t.Errorf("lines %q, want them to end %q", lines, want)
	}
	for _, p := range []string{paths.AdminState(team), paths.CtlState(team), paths.ApprovedFile(team)} {
		if w.exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	if !w.exists(paths.StatusTeamDir(team)) {
		t.Error("the status directory was removed")
	}
	before(t, w.log, "status.remove", "clean")
	if lines, err := remove(t, w, true); err != nil || lines != nil {
		t.Errorf("a second purge: lines %q, err %v", lines, err)
	}
}

// HS14: a purge after a plain remove still purges, and one with nothing to clean reports only
// what it removed.
func TestPurgeAfterRemove(t *testing.T) {
	w := applied(t)
	if _, err := remove(t, w, false); err != nil {
		t.Fatal(err)
	}
	lines, err := remove(t, w, true)
	if err != nil || !slices.Equal(lines, []string{"ctl state removed", "root state removed"}) {
		t.Errorf("lines %q, err %v", lines, err)
	}
	if w.called("clean") {
		t.Error("a team with no homeserver state was cleaned")
	}
}

// HS13, HS14: each step's failure fails the verb with the step named, is not drift, keeps the
// lines before it, and stops what follows; a later remove finishes the work.
func TestRemoveEachFailureStopsWhatFollows(t *testing.T) {
	for fault, step := range map[string]string{
		"state": StepUnit, "stop": StepUnit, "disable": StepUnit, "query": StepFirewall, "remove": StepFirewall,
		"daemon-reload": StepFiles, "status.remove": StepStatus, "clean": StepPurge,
	} {
		t.Run(fault, func(t *testing.T) {
			w := purgeable(t)
			w.faults[fault] = errFault
			lines, err := remove(t, w, true)
			e := stepOf(t, err)
			if e.Step != step || e.Drift || !errors.Is(err, errFault) || !strings.Contains(err.Error(), "team remove, step "+step+": ") {
				t.Fatalf("got %v, want step %s", err, step)
			}
			if fault == "disable" && !slices.Equal(lines, []string{"unit stopped"}) {
				t.Errorf("lines before the failure %q", lines)
			}
			stopped := map[string]bool{
				StepUnit: !w.called("query"), StepFirewall: w.exists(paths.ServerDropInFile(ops(t))),
				StepFiles: !w.called("status.remove"), StepStatus: !w.called("clean"),
				StepPurge: w.exists(paths.AdminState(ops(t))),
			}
			if !stopped[step] {
				t.Errorf("a failure at %s did not stop what follows: %q", step, w.log)
			}
			lockIsFree(t, w)
			delete(w.faults, fault)
			if _, err := remove(t, w, true); err != nil || w.exists(paths.AdminState(ops(t))) {
				t.Errorf("the next remove: %v", err)
			}
		})
	}
}

// HS13: a record or file root cannot read or remove fails its step.
func TestRemoveFileFailures(t *testing.T) {
	team := ops(t)
	for name, c := range map[string]struct {
		step  string
		spoil func(w *world)
	}{
		"firewall record": {StepFirewall, func(w *world) { w.write(paths.FirewallRecord(team), "{") }},
		"stamp": {StepFiles, func(w *world) {
			if err := os.Remove(w.at(paths.StampFile(team))); err != nil {
				t.Fatal(err)
			}
			mkdirAllOrFail(t, w.at(paths.StampFile(team)+"/x"))
		}},
		"installed": {StepInstalled, func(w *world) {
			if err := os.RemoveAll(w.at(paths.AdminStateRoot)); err != nil {
				t.Fatal(err)
			}
			w.write(paths.AdminStateRoot, "")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			c.spoil(w)
			if _, err := remove(t, w, false); stepOf(t, err).Step != c.step {
				t.Errorf("got %v, want step %s", err, c.step)
			}
			lockIsFree(t, w)
		})
	}
}

// HS2: a team lock held by another verb is busy, with the holder named.
func TestRemoveBusy(t *testing.T) {
	w := applied(t)
	held, err := TakeLock(t.Context(), w.at(paths.AdminRunDir), ops(t), "backup", 0, w.clock)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, held)
	d := w.deps()
	d.LockWait = 0
	_, err = Remove(t.Context(), d, ops(t), false)
	busy, ok := errors.AsType[*BusyError](err)
	if !ok || !strings.HasPrefix(busy.Holder, "backup ops pid=") || stepOf(t, err).Step != StepLock || stepOf(t, err).Verb != "team remove" {
		t.Errorf("got %v", err)
	}
}

func mkdirAllOrFail(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}
