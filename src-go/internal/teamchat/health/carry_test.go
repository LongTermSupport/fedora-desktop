package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SPEC-carry section 8 (SH-T1 to SH-T3): what the tick does with a team the carry has touched.
// health imports nothing of carry; the records are files the tick never reads, so their bytes
// here are placeholders and only the stamp's presence decides.

// carriedTree is a host tree with alpha applied and stamped, ops sealed (team.json and
// sealed.json, its stamp removed as SC6 removes it) and beta imported and not yet applied
// (team.json and carried.json, no stamp).
func carriedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	installTeam(t, root, "alpha", "Alpha")
	for team, record := range map[string]func(paths.TeamID) string{"ops": paths.SealRecord, "beta": paths.CarriedRecord} {
		installTeam(t, root, team, strings.ToUpper(team))
		if err := os.Remove(filepath.Join(root, paths.StampFile(id(t, team)))); err != nil {
			t.Fatal(err)
		}
		writeAt(t, filepath.Join(root, record(id(t, team))), `{"a":"carry record"}`)
	}
	return root
}

// SH-T1, SH-T3: a sealed team and an imported team before its apply have no stamp, so the
// tick's teams (Stamps, as wire gives it) never list them, and a tick writes no record of them.
func TestASealedOrImportedTeamIsNeverTicked(t *testing.T) {
	root := carriedTree(t)
	got, err := Stamps{Root: root}.Stamped(t.Context())
	if err != nil || len(got) != 1 || got[0].ID != id(t, "alpha") {
		t.Fatalf("Stamped = %+v, %v", got, err)
	}
	w := newWorld(t, "alpha")
	w.stamps = Stamps{Root: root}
	out := w.run(t)
	if len(out) != 1 || out[0].Team != id(t, "alpha") || out[0].Result != ResultWritten {
		t.Fatalf("the tick's outcomes %+v", out)
	}
	for _, team := range []string{"ops", "beta"} {
		if _, err := os.Stat(filepath.Join(w.dir, team, "status.json")); !os.IsNotExist(err) {
			t.Errorf("%s: a record was written (%v)", team, err)
		}
	}
}

// SH-T2: while a drill holds the team lock the tick drops the team as busy; once the drill has
// stopped both units (a drill killed before its restart), the tick reports each stopped unit as
// a problem and restarts neither.
func TestADrillIsNeverRestartedByTheTick(t *testing.T) {
	w := newWorld(t, "alpha")
	w.locks.busy[id(t, "alpha")] = true
	if out := w.run(t); out[0].Result != ResultDropped || !strings.Contains(out[0].Why, "busy") {
		t.Fatalf("under the drill's lock: %+v", out[0])
	}
	w.locks.busy[id(t, "alpha")] = false
	w.units.states = []UnitState{{Active: "inactive", Sub: "dead", InvocationID: "inv1"}}
	w.units.tls = UnitState{Active: "inactive", Sub: "dead", InvocationID: "tls1"}
	probes := w.prober.calls
	w.run(t)
	rec := w.record(t, "alpha")
	if !hasProblem(rec, "the homeserver unit is inactive") || w.units.restarts != 0 || w.units.tlsRestarts != 0 || w.prober.calls != probes {
		t.Fatalf("problems %v, restarts %d and %d, probes %d", whats(rec.Problems), w.units.restarts, w.units.tlsRestarts, w.prober.calls-probes)
	}
	if len(teamProblems(rec)) < 2 {
		t.Errorf("the stopped terminator is not a problem: %v", whats(rec.Problems))
	}
}
