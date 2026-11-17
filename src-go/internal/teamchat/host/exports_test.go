package host

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SPEC-carry SC4: StopTeamUnits is remove's stop, the terminator then the homeserver, each
// stopped then disabled, answering remove's lines.
func TestStopTeamUnitsIsRemovesStop(t *testing.T) {
	w := applied(t)
	w.log = nil
	lines, err := StopTeamUnits(t.Context(), w.sd, ops(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tls unit stopped", "tls unit disabled", "unit stopped", "unit disabled"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if want := []string{paths.TLSUnit(ops(t)), paths.ServerUnit(ops(t))}; !slices.Equal(w.sd.stopped, want) {
		t.Errorf("stopped %q, want %q", w.sd.stopped, want)
	}
	for _, unit := range []string{paths.TLSUnit(ops(t)), paths.ServerUnit(ops(t))} {
		if u := w.sd.unit(unit); u.Enabled || u.Active != "inactive" {
			t.Errorf("%s left %+v", unit, *u)
		}
	}
}

// SPEC-carry SD3: without disable the units are stopped and stay enabled; a unit already
// stopped is not stopped again.
func TestStopTeamUnitsWithoutDisable(t *testing.T) {
	w := applied(t)
	w.sd.unit(paths.TLSUnit(ops(t))).Active = "failed"
	w.log = nil
	lines, err := StopTeamUnits(t.Context(), w.sd, ops(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"unit stopped"}; !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if w.called("disable") || !w.sd.unit(paths.ServerUnit(ops(t))).Enabled {
		t.Errorf("a stop without disable disabled: %q", w.log)
	}
}

// SPEC-carry SC4: a failing stop is returned with the lines before it.
func TestStopTeamUnitsFails(t *testing.T) {
	w := applied(t)
	failed := errors.New("stop failed")
	w.faults["stop"] = failed
	lines, err := StopTeamUnits(t.Context(), w.sd, ops(t), true)
	if !errors.Is(err, failed) || len(lines) != 0 {
		t.Fatalf("lines %q, err %v", lines, err)
	}
}

// SPEC-carry SI6: DecodeUnits answers units.json's version, port and listen set, and refuses
// what decodeUnits refuses.
func TestDecodeUnitsIsTheRecordsFacts(t *testing.T) {
	w := applied(t)
	raw := []byte(w.read(paths.UnitsRecord(ops(t))))
	u, err := DecodeUnits(raw, ops(t))
	if err != nil {
		t.Fatal(err)
	}
	inner, err := decodeUnits(raw, ops(t))
	if err != nil {
		t.Fatal(err)
	}
	if u.Version != inner.version || u.Port != inner.port || !slices.Equal(u.Listen, inner.listen) {
		t.Errorf("DecodeUnits = %+v, units.json holds %+v", u, inner)
	}
	if _, err := DecodeUnits([]byte(`{"v":1}`), ops(t)); err == nil {
		t.Error("DecodeUnits took a record missing its fields")
	}
}

// SPEC-carry section 6: WriteRecordedDropIn writes the drop-in apply's backup start writes from
// units.json alone, under the root.
func TestWriteRecordedDropInIsApplysText(t *testing.T) {
	w := applied(t)
	u, err := DecodeUnits([]byte(w.read(paths.UnitsRecord(ops(t)))), ops(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRecordedDropIn(w.root, ops(t), u, "256M"); err != nil {
		t.Fatal(err)
	}
	want := dropInSpec{team: ops(t), version: u.Version, memoryHigh: "256M", port: u.Port}.text()
	if got := w.read(paths.ServerDropInFile(ops(t))); got != string(want) {
		t.Errorf("drop-in\n%s\nwant\n%s", got, want)
	}
}

// carried is an applied world as an import leaves it before team carry-check: both units
// stopped, no server drop-in, and the log cleared.
func carried(t *testing.T) *world {
	t.Helper()
	w := stopped(t)
	w.sd.unit(paths.TLSUnit(ops(t))).Active = "inactive"
	if err := os.Remove(w.at(paths.ServerDropInFile(ops(t)))); err != nil {
		t.Fatal(err)
	}
	return w
}

// SPEC-carry section 6: ServeLoopback writes the drop-in made from units.json alone, reloads,
// starts the homeserver on it, waits it ready on the recorded port and holds it to loopback; the
// terminator is never started.
func TestServeLoopbackStartsTheServerAloneOnTheRecordedDropIn(t *testing.T) {
	w := carried(t)
	d := w.deps()
	startedBefore := len(w.sd.started)
	if err := ServeLoopback(t.Context(), d, ops(t)); err != nil {
		t.Fatal(err)
	}
	u, err := DecodeUnits([]byte(w.read(paths.UnitsRecord(ops(t)))), ops(t))
	if err != nil {
		t.Fatal(err)
	}
	want := string(recordedSpec(ops(t), u, d.Pin.MemoryHigh).text())
	if got := w.read(paths.ServerDropInFile(ops(t))); got != want {
		t.Errorf("drop-in\n%s\nwant\n%s", got, want)
	}
	if w.sd.running != want {
		t.Errorf("the homeserver runs the drop-in\n%s\nwant\n%s", w.sd.running, want)
	}
	if got := w.sd.started[startedBefore:]; !slices.Equal(got, []string{paths.ServerUnit(ops(t))}) {
		t.Errorf("started %q, want the homeserver alone", got)
	}
	if a := w.sd.unit(paths.TLSUnit(ops(t))).Active; a != "inactive" {
		t.Errorf("the terminator is %s", a)
	}
	before(t, w.log, "daemon-reload", "start")
	before(t, w.log, "start", "ready")
	before(t, w.log, "ready", "listeners")
}

// SPEC-carry section 6: no units.json writes no drop-in and starts nothing.
func TestServeLoopbackNeedsUnitsJSON(t *testing.T) {
	w := carried(t)
	if err := os.Remove(w.at(paths.UnitsRecord(ops(t)))); err != nil {
		t.Fatal(err)
	}
	err := ServeLoopback(t.Context(), w.deps(), ops(t))
	if err == nil || stepOf(t, err).Verb != verbCarryCheck || stepOf(t, err).Step != StepUnit {
		t.Fatalf("ServeLoopback = %v", err)
	}
	if w.called("start") || w.exists(paths.ServerDropInFile(ops(t))) {
		t.Errorf("started or wrote with no units.json: %q", w.log)
	}
}

// SPEC-carry section 6: a homeserver bound past loopback, or one that fails, is the step's error.
func TestServeLoopbackFailures(t *testing.T) {
	w := carried(t)
	w.net.extra = append(w.net.extra, listener("192.0.2.10", 8448, paths.ServerUnit(ops(t))))
	if err := ServeLoopback(t.Context(), w.deps(), ops(t)); err == nil || stepOf(t, err).Step != StepAddresses {
		t.Errorf("a wider bind = %v", err)
	}
	w = carried(t)
	w.sd.startTo = "failed"
	w.sd.journal = "it fell over"
	if err := ServeLoopback(t.Context(), w.deps(), ops(t)); err == nil || stepOf(t, err).Step != StepReady {
		t.Errorf("a unit that fails = %v", err)
	}
	w = carried(t)
	w.faults["daemon-reload"] = errFault
	if err := ServeLoopback(t.Context(), w.deps(), ops(t)); !errors.Is(err, errFault) || stepOf(t, err).Step != StepUnit {
		t.Errorf("a failing reload = %v", err)
	}
}
