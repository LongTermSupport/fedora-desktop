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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
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

// SPEC-carry sc-D14: RemoveDropIn deletes the drop-in and its directory when nothing else is in
// it, answers false for none, keeps a directory another file is in, and fails on an entry it
// cannot remove.
func TestRemoveDropIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unit.d")
	file := filepath.Join(dir, "team.conf")
	put := func(path string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[Service]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put(file)
	if gone, err := RemoveDropIn(file); !gone || err != nil {
		t.Fatalf("gone %v, err %v", gone, err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the empty directory is left: %v", err)
	}
	if gone, err := RemoveDropIn(file); gone || err != nil {
		t.Errorf("none: gone %v, err %v", gone, err)
	}
	put(file)
	put(filepath.Join(dir, "owner.conf"))
	if gone, err := RemoveDropIn(file); !gone || err != nil {
		t.Errorf("beside another file: gone %v, err %v", gone, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "owner.conf")); err != nil {
		t.Errorf("the owner's file: %v", err)
	}
	put(filepath.Join(file, "inside"))
	if _, err := RemoveDropIn(file); err == nil {
		t.Error("a directory with entries at the drop-in's name was removed")
	}
}

// LeafError carries its cause and wraps the step's error.
func TestLeafErrorWraps(t *testing.T) {
	failed := errors.New("no key")
	err := leafFailed("the leaf key", failed)
	if !errors.Is(err, failed) || err.Error() != "the leaf key: no key" || leafFailed("x", nil) != nil {
		t.Errorf("err = %v", err)
	}
}

// lxcRecord writes the team's LXC record with one seat of the given status.
func lxcRecord(w *world, status string) {
	if err := os.MkdirAll(w.at(paths.LXCRecordDir), 0o750); err != nil {
		w.t.Fatal(err)
	}
	w.write(paths.LXCRecord(ops(w.t)), `{"v":1,"team":"ops","members":[{"container":"box-1","checkout":"/srv/checkouts/thing","seat":1,`+
		`"user":"agent","handle":"thing.1+desk.lxc","status":"`+status+`","since_ms":1,"why":""}]}`)
}

// C 3.9.2: a seat the team's LXC record holds that has not left is live in its guest, with a
// kit, a bundle and maybe a session unit only `lxc apply --phase absent` removes, and a token
// only its park revokes. remove (with or without --purge) refuses such a team before any change,
// naming the seat and the way out; seats only left (parked, guest files pending) do not stop it.
func TestRemoveRefusesATeamWithLiveLXCSeats(t *testing.T) {
	for _, purge := range []bool{false, true} {
		w := applied(t)
		lxcRecord(w, "joined")
		lines, err := remove(t, w, purge)
		var herr *Error
		if !errors.As(err, &herr) || herr.Step != StepLXCSeats || !herr.Drift ||
			!strings.Contains(err.Error(), "box-1/thing-1") || !strings.Contains(err.Error(), "lxc apply --phase absent") {
			t.Fatalf("purge %v: err %v", purge, err)
		}
		if lines != nil || w.called("stop") || !w.exists(paths.StampFile(ops(t))) {
			t.Fatalf("purge %v: a refused remove changed something: lines %q, log %q", purge, lines, w.log)
		}
		lockIsFree(t, w)
		lxcRecord(w, "left")
		if _, err := remove(t, w, purge); err != nil {
			t.Fatalf("purge %v: seats only left: %v", purge, err)
		}
	}
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

// HS14, X18: remove stops and disables the terminator, then the homeserver, removes the recorded
// rules in both configurations, both drop-ins and the stamp, and hands the status record to its
// writer; the team's data stays.
func TestRemove(t *testing.T) {
	w := applied(t)
	w.status.removeLine = "status record removed"
	team := ops(t)
	makeDatabase(t, w)
	linkState(t, w)
	lines, err := remove(t, w, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tls unit stopped", "tls unit disabled", "unit stopped", "unit disabled",
		"firewall removed public 192.0.2.0/24 tcp:8448", "firewall removed public 192.0.2.10/32 tcp:8448",
		"tls drop-in removed", "drop-in removed", "stamp removed", "status record removed",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q\nwant %q", lines, want)
	}
	for _, p := range []string{
		paths.ServerDropInFile(team), filepath.Dir(paths.ServerDropInFile(team)), paths.TLSDropInFile(team),
		filepath.Dir(paths.TLSDropInFile(team)), paths.FirewallRecord(team), paths.StampFile(team),
	} {
		if w.exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	for _, p := range []string{
		paths.AdminState(team), paths.TLSDir(team), paths.CtlState(team), paths.ServerConfig(team),
		paths.ServerNameFile(team), paths.UnitsRecord(team), paths.TeamDeclaration(team),
		paths.HomeserverState(team), paths.HomeserverPrivateDatabase(team),
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
	lockIsFree(t, w)
	if lines, err := remove(t, w, false); err != nil || lines != nil {
		t.Errorf("a second remove: lines %q, err %v", lines, err)
	}
}

// HS14: a unit already stopped and disabled is left alone, the terminator's as the homeserver's.
func TestRemoveStoppedUnit(t *testing.T) {
	w := applied(t)
	*w.sd.unit(paths.ServerUnit(ops(t))) = UnitState{Active: "inactive"}
	*w.sd.unit(paths.TLSUnit(ops(t))) = UnitState{Active: "inactive"}
	lines, err := remove(t, w, false)
	if err != nil || slices.Contains(lines, "unit stopped") || slices.Contains(lines, "unit disabled") ||
		slices.Contains(lines, "tls unit stopped") || slices.Contains(lines, "tls unit disabled") {
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
	// The apply also made the bus address's host rule, which the record written above does not name.
	unnamed := Rule{Zone: "public", Source: netip.MustParsePrefix("192.0.2.10/32"), Port: 8448}
	for _, permanent := range []bool{true, false} {
		if !w.fw.present[permanent][other.String()] || !w.fw.present[permanent][unnamed.String()] || len(w.fw.present[permanent]) != 2 {
			t.Errorf("permanent %v: rules left %v", permanent, w.fw.present[permanent])
		}
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

// linkState makes the homeserver's state as systemd keeps it for a DynamicUser: the state
// directory a link to the private one.
func linkState(t *testing.T, w *world) {
	t.Helper()
	mkdirAllOrFail(t, w.at(paths.HomeserverStateRoot))
	if err := os.Symlink(w.at(paths.HomeserverPrivateState(ops(t))), w.at(paths.HomeserverState(ops(t)))); err != nil {
		t.Fatal(err)
	}
}

// noSystemdAfterTheDropIns holds that nothing asked systemd about the team once its drop-ins
// were gone: the template unit has no ExecStart= without the drop-in, so systemd refuses any
// act on the team's unit ("bad unit file setting"), and a purge must not depend on one.
func noSystemdAfterTheDropIns(t *testing.T, log []string) {
	t.Helper()
	at := slices.Index(log, "status.remove")
	if at < 0 {
		t.Fatalf("no status.remove in %q", log)
	}
	if after := log[at+1:]; len(after) != 0 {
		t.Errorf("seam calls after the drop-ins were removed: %q", after)
	}
}

// HS14: --purge also removes the homeserver's state (the private directory and systemd's link to
// it, with no systemd act: the unit is undefined once its drop-in is gone), then ctl/<team>/, the
// approval and root/<team>/, the last of them last; the status directory stays for the status
// record's writer.
func TestRemovePurge(t *testing.T) {
	w := purgeable(t)
	linkState(t, w)
	team := ops(t)
	lines, err := remove(t, w, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"homeserver state cleaned", "ctl state removed", "approval removed", "leaf removed", "root state removed"}
	if len(lines) < len(want) || !slices.Equal(lines[len(lines)-len(want):], want) {
		t.Errorf("lines %q, want them to end %q", lines, want)
	}
	for _, p := range []string{
		paths.AdminState(team), paths.TLSDir(team), paths.CtlState(team), paths.ApprovedFile(team),
		paths.HomeserverState(team), paths.HomeserverPrivateState(team),
	} {
		if w.exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	if !w.exists(paths.StatusTeamDir(team)) || !w.exists(paths.HomeserverPrivateStateRoot) || !w.exists(paths.HomeserverStateRoot) {
		t.Error("a directory every team shares was removed")
	}
	noSystemdAfterTheDropIns(t, w.log)
	if lines, err := remove(t, w, true); err != nil || lines != nil {
		t.Errorf("a second purge: lines %q, err %v", lines, err)
	}
}

// HS14, n2d-H16: a purge after a plain remove still purges, the homeserver's state included,
// though the drop-in went with the plain remove; one with no state reports only what it removed.
func TestPurgeAfterRemove(t *testing.T) {
	for name, c := range map[string]struct {
		state bool
		want  []string
	}{
		"with state":    {true, []string{"homeserver state cleaned", "ctl state removed", "leaf removed", "root state removed"}},
		"with no state": {false, []string{"ctl state removed", "leaf removed", "root state removed"}},
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			if c.state {
				makeDatabase(t, w)
				linkState(t, w)
			}
			if _, err := remove(t, w, false); err != nil {
				t.Fatal(err)
			}
			w.log = nil
			lines, err := remove(t, w, true)
			if err != nil || !slices.Equal(lines, c.want) {
				t.Errorf("lines %q, err %v", lines, err)
			}
			if w.exists(paths.HomeserverPrivateState(ops(t))) || w.exists(paths.HomeserverState(ops(t))) {
				t.Error("the homeserver's state is still there")
			}
		})
	}
}

// HS14: the state directory is removed as what it is: a link is removed and never followed, so
// what it points to outside the private directory stays; a real directory is removed whole.
func TestPurgeRemovesTheStateDirectoryAsItIs(t *testing.T) {
	t.Run("a link elsewhere", func(t *testing.T) {
		w := purgeable(t)
		outside := filepath.Join(t.TempDir(), "keep")
		if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mkdirAllOrFail(t, w.at(paths.HomeserverStateRoot))
		if err := os.Symlink(filepath.Dir(outside), w.at(paths.HomeserverState(ops(t)))); err != nil {
			t.Fatal(err)
		}
		if _, err := remove(t, w, true); err != nil {
			t.Fatal(err)
		}
		if w.exists(paths.HomeserverState(ops(t))) {
			t.Error("the link is still there")
		}
		if _, err := os.Stat(outside); err != nil {
			t.Errorf("the link was followed: %v", err)
		}
	})
	t.Run("a real directory", func(t *testing.T) {
		w := applied(t)
		mkdirAllOrFail(t, w.at(paths.HomeserverState(ops(t))+"/db"))
		lines, err := remove(t, w, true)
		if err != nil || !slices.Contains(lines, "homeserver state cleaned") || w.exists(paths.HomeserverState(ops(t))) {
			t.Errorf("lines %q, err %v", lines, err)
		}
	})
}

// HS13, HS14: each step's failure fails the verb with the step named, is not drift, keeps the
// lines before it, and stops what follows; a later remove finishes the work.
func TestRemoveEachFailureStopsWhatFollows(t *testing.T) {
	for fault, step := range map[string]string{
		"state": StepUnit, "stop": StepUnit, "disable": StepUnit, "query": StepFirewall, "remove": StepFirewall,
		"daemon-reload": StepFiles, "status.remove": StepStatus,
	} {
		t.Run(fault, func(t *testing.T) {
			w := purgeable(t)
			w.faults[fault] = errFault
			lines, err := remove(t, w, true)
			e := stepOf(t, err)
			if e.Step != step || e.Drift || !errors.Is(err, errFault) || !strings.Contains(err.Error(), "team remove, step "+step+": ") {
				t.Fatalf("got %v, want step %s", err, step)
			}
			if fault == "disable" && !slices.Equal(lines, []string{"tls unit stopped"}) {
				t.Errorf("lines before the failure %q", lines)
			}
			stopped := map[string]bool{
				StepUnit: !w.called("query"), StepFirewall: w.exists(paths.ServerDropInFile(ops(t))),
				StepFiles: !w.called("status.remove"), StepStatus: w.exists(paths.HomeserverPrivateState(ops(t))),
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
		"homeserver state": {StepPurge, func(w *world) { w.write(paths.HomeserverStateRoot, "") }},
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			c.spoil(w)
			if _, err := remove(t, w, c.step == StepPurge); stepOf(t, err).Step != c.step {
				t.Errorf("got %v, want step %s", err, c.step)
			}
			lockIsFree(t, w)
		})
	}
}

// HS2: a team lock held by another verb is busy, with the holder named.
func TestRemoveBusy(t *testing.T) {
	w := applied(t)
	held, err := teamlock.Take(t.Context(), w.at(paths.AdminRunDir), ops(t), "backup", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOrFail(t, held)
	d := w.deps()
	d.LockWait = 0
	_, err = Remove(t.Context(), d, ops(t), false)
	busy, ok := errors.AsType[*teamlock.BusyError](err)
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

// RG3, SG4: Remove runs Deps.RemoveGuard under the lock, not Deps.Guard; its refusal stops
// every step and frees the lock.
func TestRemoveIsRefusedByTheGuard(t *testing.T) {
	w := applied(t)
	w.log, w.guarded, w.removeGuarded = nil, nil, nil
	refused := errors.New("a restore is under way")
	w.removeGuardErr = refused
	lines, err := Remove(t.Context(), w.deps(), ops(t), true)
	if !errors.Is(err, refused) || stepOf(t, err).Step != StepLock || len(lines) != 0 {
		t.Fatalf("lines %q, err %v", lines, err)
	}
	if len(w.log) != 0 || !slices.Equal(w.removeGuarded, []paths.TeamID{ops(t)}) || len(w.guarded) != 0 {
		t.Errorf("log %q, remove guarded %v, guarded %v", w.log, w.removeGuarded, w.guarded)
	}
	lockIsFree(t, w)
}

// SG4: a team every other verb's guard refuses (a sealed one) is still removed: Remove never
// asks Deps.Guard.
func TestRemoveIgnoresTheOtherVerbsGuard(t *testing.T) {
	w := applied(t)
	w.guarded = nil
	w.guardErr = errors.New("team ops is sealed")
	if _, err := Remove(t.Context(), w.deps(), ops(t), true); err != nil {
		t.Fatal(err)
	}
	if len(w.guarded) != 0 {
		t.Errorf("Remove asked Deps.Guard for %v", w.guarded)
	}
}
