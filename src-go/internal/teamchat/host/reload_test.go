package host

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// narrowed is the sample input with allow_from narrowed, a reload-set change, approved in w.
func narrowed(w *world) declare.Input {
	doc := teamDoc()
	docTeam(doc)["allow_from"] = []any{"203.0.113.0/24"}
	return approved(w, doc)
}

// stampConfig is the team.json digest the stamp records.
func stampConfig(t *testing.T, w *world) string {
	t.Helper()
	st, err := decodeStamp([]byte(w.read(paths.StampFile(ops(t)))), ops(t))
	if err != nil {
		t.Fatal(err)
	}
	return st.config
}

// loadedIsOnDisk holds the drop-in systemd loaded to the one on disk.
func loadedIsOnDisk(t *testing.T, w *world) {
	t.Helper()
	if disk := w.read(paths.ServerDropInFile(ops(t))); w.sd.loaded != disk {
		t.Errorf("systemd loaded\n%s\nthe drop-in on disk is\n%s", w.sd.loaded, disk)
	}
}

// failsAtWithoutStamp applies in, which must fail at step and leave the stamp recording was.
func failsAtWithoutStamp(t *testing.T, w *world, in declare.Input, step, was string) {
	t.Helper()
	if _, err := w.apply(in); stepOf(t, err).Step != step {
		t.Fatalf("got %v, want step %s", err, step)
	}
	if stampConfig(t, w) != was {
		t.Fatal("the team was stamped applied before systemd reloaded its drop-in")
	}
}

// crashed puts on disk the drop-in an apply of in renders, and leaves the rest as it is: what an
// apply that died between the drop-in's write and its daemon-reload leaves behind.
func crashed(t *testing.T, w *world, in declare.Input) {
	t.Helper()
	other := newWorld(t)
	other.version = w.version
	other.approve(in)
	if _, err := other.apply(in); err != nil {
		t.Fatal(err)
	}
	w.write(paths.ServerDropInFile(ops(t)), other.read(paths.ServerDropInFile(ops(t))))
}

// HS15: a daemon-reload that failed after the drop-in was written is retried by every later
// apply, and none stamps the team applied until one has succeeded.
func TestAFailedReloadIsRetried(t *testing.T) {
	w := applied(t)
	was := stampConfig(t, w)
	in := narrowed(w)
	w.faults["daemon-reload"] = errFault
	failsAtWithoutStamp(t, w, in, StepUnit, was)
	failsAtWithoutStamp(t, w, in, StepUnit, was)
	delete(w.faults, "daemon-reload")
	lines, err := w.apply(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"team.json written", "units.json written", "stamp written"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if !w.called("daemon-reload") || w.called("restart") {
		t.Errorf("calls %q", w.log)
	}
	loadedIsOnDisk(t, w)
}

// HS15: a crash between the drop-in's write and its daemon-reload is finished by the next
// apply, which reloads before it stamps.
func TestADropInLeftByACrashIsReloaded(t *testing.T) {
	w := applied(t)
	was := stampConfig(t, w)
	in := narrowed(w)
	crashed(t, w, in)
	w.faults["daemon-reload"] = errFault
	failsAtWithoutStamp(t, w, in, StepUnit, was)
	delete(w.faults, "daemon-reload")
	if _, err := w.apply(in); err != nil {
		t.Fatal(err)
	}
	if !w.called("daemon-reload") || stampConfig(t, w) == was {
		t.Errorf("calls %q", w.log)
	}
	loadedIsOnDisk(t, w)
}

// HS15: an upgrade whose reload failed, or that crashed before it, is reloaded before the next
// apply restarts the unit, so the new version is the one that runs.
func TestAnUpgradeIsReloadedBeforeItsRestart(t *testing.T) {
	for name, interrupt := range map[string]func(t *testing.T, w *world){
		"failed reload": func(t *testing.T, w *world) {
			t.Helper()
			w.faults["daemon-reload"] = errFault
			if _, err := w.apply(w.cur); stepOf(t, err).Step != StepUnit {
				t.Fatalf("got %v", err)
			}
			delete(w.faults, "daemon-reload")
		},
		"crash": func(t *testing.T, w *world) {
			t.Helper()
			crashed(t, w, w.cur)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			w.version = "1.9.5"
			interrupt(t, w)
			lines, err := w.apply(w.cur)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(lines, "unit restarted") {
				t.Errorf("lines %q", lines)
			}
			before(t, w.log, "daemon-reload", "restart")
			if !strings.Contains(w.sd.running, "tuwunel-1.9.5") {
				t.Errorf("systemd runs\n%s", w.sd.running)
			}
			loadedIsOnDisk(t, w)
		})
	}
}

// HS15: a changed base unit is reloaded before the restart that takes it.
func TestABaseUnitChangeIsReloadedBeforeItsRestart(t *testing.T) {
	w := applied(t)
	w.write(paths.ServerUnitTemplateFile, "[Unit]\nDescription=a new base unit\n")
	if _, err := w.apply(w.cur); err != nil {
		t.Fatal(err)
	}
	before(t, w.log, "daemon-reload", "restart")
}

// HS15: the drop-in written for a backup whose reload failed is reloaded by the next apply
// before the backup start, so the backup is taken on the version that wrote the database.
func TestABackupDropInWhoseReloadFailedIsReloaded(t *testing.T) {
	w := applied(t)
	makeDatabase(t, w)
	w.version = "1.9.5"
	w.faults["restart"] = errFault
	if _, err := w.apply(w.cur); stepOf(t, err).Step != StepUnit {
		t.Fatalf("got %v", err)
	}
	delete(w.faults, "restart")
	*w.sd.unit(paths.ServerUnit(ops(t))) = UnitState{Active: "inactive"}
	w.faults["daemon-reload"] = errFault
	if _, err := w.apply(w.cur); stepOf(t, err).Step != StepBackup {
		t.Fatalf("got %v", err)
	}
	delete(w.faults, "daemon-reload")
	w.onBackup = func() {
		if !strings.Contains(w.sd.running, "tuwunel-1.9.4") {
			t.Errorf("the backup ran on\n%s", w.sd.running)
		}
	}
	if _, err := w.apply(w.cur); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.sd.running, "tuwunel-1.9.5") {
		t.Errorf("systemd runs\n%s", w.sd.running)
	}
}

// HS14, HS15: a drop-in whose removal's daemon-reload failed is unloaded by the next remove.
func TestRemoveRetriesAFailedReload(t *testing.T) {
	w := applied(t)
	w.faults["daemon-reload"] = errFault
	if _, err := remove(t, w, false); stepOf(t, err).Step != StepFiles {
		t.Fatalf("got %v", err)
	}
	delete(w.faults, "daemon-reload")
	if _, err := remove(t, w, false); err != nil {
		t.Fatal(err)
	}
	if w.sd.loaded != "" {
		t.Errorf("systemd still has a removed drop-in loaded:\n%s", w.sd.loaded)
	}
}
