package host

import (
	"errors"
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
