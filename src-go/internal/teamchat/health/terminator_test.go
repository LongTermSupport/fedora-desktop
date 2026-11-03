package health

import (
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// X18c: members reach a team's homeserver through its TLS terminator, so a terminator that is
// not active is a problem on the record even while the homeserver answers on loopback. The tick
// reports it and restarts nothing: systemd restarts the unit always, so one that is not active
// was stopped or failed past its start limit.
func TestAStoppedTerminatorIsAProblemWhileTheHomeserverIsWell(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.tls = UnitState{Active: "inactive", Sub: "dead"}
	if out := w.run(t); out[0].Result != ResultWritten {
		t.Fatalf("outcome %+v", out[0])
	}
	rec := w.record(t, "alpha")
	unit := paths.TLSUnit(id(t, "alpha"))
	want := status.Problem{What: "the TLS terminator unit is inactive: no member can reach the homeserver", Fix: "systemctl status " + unit}
	if !slices.Contains(rec.Problems, want) {
		t.Fatalf("problems %v, want %+v", rec.Problems, want)
	}
	if rec.Homeserver.Probe != status.ProbeOK || rec.Homeserver.Unit != "active" || w.units.restarts != 0 {
		t.Fatalf("the homeserver is judged by its own unit: probe %s, unit %s, %d restarts", rec.Homeserver.Probe, rec.Homeserver.Unit, w.units.restarts)
	}
	if !slices.Equal(w.units.tlsAsked, []string{unit}) {
		t.Fatalf("units asked about: %v", w.units.tlsAsked)
	}
}

// A failed unit is named as failed: the word is systemd's own.
func TestAFailedTerminatorIsNamedFailed(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.tls = UnitState{Active: "failed", Sub: "failed"}
	w.run(t)
	rec := w.record(t, "alpha")
	if !hasProblem(rec, "the TLS terminator unit is failed") {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

// A terminator whose state cannot be read is a problem of its own, as the homeserver's is, and
// does not fail the team's tick.
func TestAnUnreadableTerminatorIsAProblemNotAFailure(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.tlsErr = errors.New("dbus down")
	if out := w.run(t); out[0].Result != ResultWritten {
		t.Fatalf("outcome %+v", out[0])
	}
	rec := w.record(t, "alpha")
	want := status.Problem{What: "the TLS terminator unit's state cannot be read", Fix: "systemctl status " + paths.TLSUnit(id(t, "alpha"))}
	if !slices.Contains(rec.Problems, want) {
		t.Fatalf("problems %v, want %+v", rec.Problems, want)
	}
}

// An active terminator is no problem, and is asked about once, by the team's own unit name.
func TestARunningTerminatorIsNoProblem(t *testing.T) {
	w := newWorld(t, "alpha", "beta")
	w.run(t)
	if rec := w.record(t, "alpha"); hasProblem(rec, "the TLS terminator") {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
	asked := slices.Clone(w.units.tlsAsked)
	slices.Sort(asked)
	want := []string{paths.TLSUnit(id(t, "alpha")), paths.TLSUnit(id(t, "beta"))}
	if !slices.Equal(asked, want) {
		t.Fatalf("units asked about: %v, want %v", asked, want)
	}
}

// A homeserver that is down does not hide a terminator that is down: both stand on the record.
func TestBothUnitsDownAreBothOnTheRecord(t *testing.T) {
	w := newWorld(t, "alpha")
	w.units.states = []UnitState{{Active: "inactive", Sub: "dead"}}
	w.units.tls = UnitState{Active: "inactive", Sub: "dead"}
	w.run(t)
	rec := w.record(t, "alpha")
	if !hasProblem(rec, "the homeserver unit is inactive") || !hasProblem(rec, "the TLS terminator unit is inactive") {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

// HB3: a recorded restore stops units on purpose, so the terminator is not judged while one is.
func TestTheTerminatorIsNotJudgedWhileARestoreIsRecorded(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.state = interrupted()
	w.units.tls = UnitState{Active: "inactive", Sub: "dead"}
	w.run(t)
	rec := w.record(t, "alpha")
	if hasProblem(rec, "the TLS terminator") || len(w.units.tlsAsked) != 0 {
		t.Fatalf("problems %v, units asked about %v", whats(rec.Problems), w.units.tlsAsked)
	}
}
