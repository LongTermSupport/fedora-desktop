package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// sealUnderLock is a seal's effect on the tick's world (SPEC-carry SC6): under the team lock,
// the stamp removed. It counts each run in runs.
func sealUnderLock(t *testing.T, w *world, team paths.TeamID, runs *int) func() {
	t.Helper()
	return func() {
		lock, err := teamlock.Take(context.Background(), w.locks.dir, team, "team.seal", 0, w.clock, teamlock.Unguarded)
		if err != nil {
			t.Errorf("the seal's lock: %v", err)
			return
		}
		w.teams.removed[team] = true
		*runs++
		if err := lock.Release(); err != nil {
			t.Errorf("the seal's release: %v", err)
		}
	}
}

// SPEC-carry SL2: a seal lands between a tick's gather and the work it does under the team
// lock. The seal holds the lock while it takes the team off the books; the tick, which takes
// the lock unguarded and without waiting, then re-reads the stamp, finds none and drops the
// team: no restart, no leaf renewal, no record written.
func TestASealBetweenGatherAndUnderLockDropsTheTeam(t *testing.T) {
	for name, c := range map[string]struct {
		// fails is how many probes fail; the seal lands during probe number at.
		fails, at int
	}{
		"before the restart":      {fails: 3, at: 3},
		"before the leaf renewal": {fails: 0, at: 1},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, "alpha")
			renewals, ok := w.leaves.(*leaves)
			if !ok {
				t.Fatal("the world's leaves are not the counting fake")
			}
			seals := 0
			seal := sealUnderLock(t, w, id(t, "alpha"), &seals)
			w.prober.fails = c.fails
			w.prober.on = func(call int) {
				if call == c.at {
					seal()
				}
			}
			out := w.run(t)
			if seals != 1 || out[0].Result != ResultDropped || out[0].Why != errRemoved.Error() {
				t.Errorf("%d seals, outcome %+v", seals, out[0])
			}
			if w.units.restarts != 0 || renewals.calls != 0 {
				t.Errorf("after the seal: %d restarts, %d leaf renewals", w.units.restarts, renewals.calls)
			}
			if _, err := os.Stat(filepath.Join(w.dir, "alpha", "status.json")); !errors.Is(err, os.ErrNotExist) {
				t.Error("a sealed team's record was written")
			}
		})
	}
}
