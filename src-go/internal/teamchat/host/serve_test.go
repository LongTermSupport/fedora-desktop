package host

import (
	"errors"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// stopped is an applied world whose unit has been stopped, with the log cleared.
func stopped(t *testing.T) *world {
	t.Helper()
	w := applied(t)
	w.sd.unit(paths.ServerUnit(ops(t))).Active = "inactive"
	w.log = nil
	return w
}

// SM3: Serve reloads, starts a stopped unit, waits for it ready and checks its bound
// addresses, all from units.json.
func TestServeStartsAStoppedTeam(t *testing.T) {
	w := stopped(t)
	if err := Serve(t.Context(), w.deps(), ops(t)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"daemon-reload", "start", "ready", "listeners"} {
		if !w.called(step) {
			t.Errorf("Serve did not %s: %q", step, w.log)
		}
	}
	before(t, w.log, "daemon-reload", "start")
	before(t, w.log, "start", "ready")
	before(t, w.log, "ready", "listeners")
	if w.sd.units[paths.ServerUnit(ops(t))].Active != "active" {
		t.Fatal("the unit is not active")
	}
}

// SM3: a running unit is not started again, and is still checked ready.
func TestServeLeavesARunningUnit(t *testing.T) {
	w := applied(t)
	w.log = nil
	if err := Serve(t.Context(), w.deps(), ops(t)); err != nil {
		t.Fatal(err)
	}
	if w.called("start") || w.called("restart") || !w.called("ready") {
		t.Fatalf("log %q", w.log)
	}
}

// SM3: Serve never renders: the config, the drop-in and the records are as they were, even
// when the pin has moved on since the apply.
func TestServeNeverRenders(t *testing.T) {
	w := stopped(t)
	files := []string{
		paths.ServerConfig(ops(t)), paths.ServerDropInFile(ops(t)), paths.UnitsRecord(ops(t)),
		paths.TeamDeclaration(ops(t)), paths.StampFile(ops(t)),
	}
	was := map[string]string{}
	for _, f := range files {
		was[f] = w.read(f)
	}
	w.version = "1.9.5"
	if err := Serve(t.Context(), w.deps(), ops(t)); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if w.read(f) != was[f] {
			t.Errorf("%s changed", f)
		}
	}
}

// SM3: the port and listen set are units.json's, not a declaration's.
func TestServeWaitsOnTheRecordedPort(t *testing.T) {
	w := stopped(t)
	w.net.bound = []Listener{listener("127.0.0.1", 9999, paths.ServerUnit(ops(t)))}
	err := Serve(t.Context(), w.deps(), ops(t))
	if err == nil || stepOf(t, err).Step != StepAddresses || stepOf(t, err).Verb != verbServe {
		t.Fatalf("Serve with the wrong port bound = %v", err)
	}
}

// SM3: no units.json is a team never applied; nothing is started.
func TestServeNeedsUnitsJSON(t *testing.T) {
	w := stopped(t)
	if err := os.Remove(w.at(paths.UnitsRecord(ops(t)))); err != nil {
		t.Fatal(err)
	}
	err := Serve(t.Context(), w.deps(), ops(t))
	if err == nil || stepOf(t, err).Step != StepUnit || w.called("start") {
		t.Fatalf("Serve = %v, log %q", err, w.log)
	}
}

// SM3: a units.json that does not decode is an error, and nothing is started.
func TestServeRefusesABadUnitsJSON(t *testing.T) {
	w := stopped(t)
	w.write(paths.UnitsRecord(ops(t)), "{}")
	if err := Serve(t.Context(), w.deps(), ops(t)); err == nil || w.called("start") {
		t.Fatalf("Serve = %v, log %q", err, w.log)
	}
}

// SM3: each effect's failure is Serve's, at its step.
func TestServeFailures(t *testing.T) {
	for fault, step := range map[string]string{
		"daemon-reload": StepUnit, "state": StepUnit, "start": StepUnit, "listeners": StepAddresses,
	} {
		w := stopped(t)
		w.faults[fault] = errFault
		err := Serve(t.Context(), w.deps(), ops(t))
		if !errors.Is(err, errFault) || stepOf(t, err).Step != step {
			t.Errorf("%s failing: %v", fault, err)
		}
	}
	w := stopped(t)
	w.sd.startTo = "failed"
	w.sd.journal = "it fell over"
	if err := Serve(t.Context(), w.deps(), ops(t)); err == nil || stepOf(t, err).Step != StepReady {
		t.Fatalf("a unit that fails = %v", err)
	}
}
