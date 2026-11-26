package restore

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// stagedDuring runs a fresh restore whose OnStaged brings the staged copy up with Staged and
// runs probe while it serves.
func stagedDuring(w *world, probe func(ctx context.Context, check func(context.Context) (Epoch, error)) error) (Answer, error) {
	d := w.deps()
	d.OnStaged = func(ctx context.Context, team paths.TeamID) error {
		if err := w.effect("onstaged"); err != nil {
			return err
		}
		return Staged(ctx, d, team, probe)
	}
	return Run(context.Background(), d, nil, w.request())
}

// With the copy staged, Staged opens it as it is (no --restore-backup), the check goes through
// the secure unit and reads the secured copy's epoch, the unit is stopped and unloaded again,
// and the restore then swaps the copy in as it would have.
func TestStagedBringsTheStagedCopyUpForTheProbe(t *testing.T) {
	w := newWorld(t)
	var got Epoch
	_, err := stagedDuring(w, func(ctx context.Context, check func(context.Context) (Epoch, error)) error {
		if err := w.effect("probe"); err != nil {
			return err
		}
		var cerr error
		got, cerr = check(ctx)
		return cerr
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != w.secured || got.RestoreEpoch == 0 {
		t.Errorf("the joined check read %+v, not the secured copy's %+v", got, w.secured)
	}
	at := slices.Index(w.calls, "onstaged")
	want := []string{"onstaged", "run staging restoring=false", "probe", "child check staged=true", "stop " + paths.RestoreUnit(w.team)}
	if at < 0 || len(w.calls) < at+len(want) || !slices.Equal(w.calls[at:at+len(want)], want) {
		t.Errorf("calls =\n%q\nwant from onstaged\n%q", w.calls, want)
	}
	w.restored()
}

// A probe's failure is the hook's: the unit is still stopped, and the restore stays staged.
func TestStagedStopsTheUnitAfterAFailedProbe(t *testing.T) {
	w := newWorld(t)
	_, err := stagedDuring(w, func(context.Context, func(context.Context) (Epoch, error)) error {
		return errors.New("the probe failed")
	})
	errWith(t, err, "restore not done, staged", "the probe failed")
	if i := slices.Index(w.calls, "onstaged"); i < 0 || !slices.Contains(w.calls[i:], "stop "+paths.RestoreUnit(w.team)) {
		t.Errorf("the staging unit was left running: %q", w.calls)
	}
	w.staged()
}

// A staging unit that restarted while the probe ran is not the copy probed: refused.
func TestStagedRefusesAUnitThatRestartedDuringTheProbe(t *testing.T) {
	w := newWorld(t)
	_, err := stagedDuring(w, func(context.Context, func(context.Context) (Epoch, error)) error {
		w.unit(paths.RestoreUnit(w.team)).st.NRestarts = 1
		return nil
	})
	errWith(t, err, "restore not done, staged", "is not the run it was")
	w.staged()
}

// With no restore staged there is no copy to bring up.
func TestStagedRefusesWithoutAStagedRestore(t *testing.T) {
	w := newWorld(t)
	err := Staged(t.Context(), w.deps(), w.team, func(context.Context, func(context.Context) (Epoch, error)) error {
		t.Error("the probe ran with nothing staged")
		return nil
	})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Staged = %v, want a refusal", err)
	}
}

// Owners is the owner of every entry of the team's state directory, by its name beneath it, a
// link's own and not its target's.
func TestOSOwnersWalksTheStateDirectoryWithoutFollowingALink(t *testing.T) {
	o, state := osTree(t)
	writeFile(t, state+"/db/000001.sst", "x")
	writeFile(t, state+"/db/CURRENT", "x")
	mkdirs(t, state+"/backups/meta")
	if err := os.Symlink("/etc", state+"/link"); err != nil {
		t.Fatal(err)
	}
	got, err := o.Owners(opsTeam(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"backups", "backups/meta", "db", "db/000001.sst", "db/CURRENT", "link"}
	names := make([]string, 0, len(got))
	for name, uid := range got {
		names = append(names, name)
		if uid != myUID(t) {
			t.Errorf("%s is owned by %d", name, uid)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Errorf("Owners names = %q, want %q", names, want)
	}
}

// A state directory that is not the trusted one's is refused, as every other read of it is.
func TestOSOwnersRefusesAnUntrustedStateRoot(t *testing.T) {
	o, state := osTree(t)
	writeFile(t, state+"/db/CURRENT", "x")
	o.StateOwnerUID++
	if got, err := o.Owners(opsTeam(t)); err == nil {
		t.Fatalf("Owners read a state root of another owner: %v", got)
	}
}
