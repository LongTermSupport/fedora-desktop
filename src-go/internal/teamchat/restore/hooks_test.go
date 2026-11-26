package restore

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// hooked runs a fresh restore with the drill's hooks set, each recorded as an effect.
func hooked(w *world, onStaged, afterSwap bool) (Answer, error) {
	d := w.deps()
	if onStaged {
		d.OnStaged = func(_ context.Context, team paths.TeamID) error {
			if team != w.team {
				return errors.New("OnStaged of another team")
			}
			return w.effect("onstaged")
		}
	}
	if afterSwap {
		d.AfterSwap = func(_ context.Context, team paths.TeamID) error {
			if team != w.team {
				return errors.New("AfterSwap of another team")
			}
			return w.effect("afterswap")
		}
	}
	return Run(context.Background(), d, nil, w.request())
}

// A real restore sets neither hook, and its effects are happyCalls (TestRunRestoresOnline); the
// drill's hooks run where the drill probes: OnStaged with the copy staged, after the export and
// before the epoch is raised; AfterSwap after the child's swap, before the record reaches start.
func TestTheDrillHooksRunAtStagedAndAfterTheSwap(t *testing.T) {
	w := newWorld(t)
	if _, err := hooked(w, true, true); err != nil {
		t.Fatal(err)
	}
	want := happyCalls(w)
	want = slices.Insert(want, slices.Index(want, "write restore-epoch.json"), "onstaged")
	want = slices.Insert(want, slices.Index(want, "write restore.json=start"), "afterswap")
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", w.calls, want)
	}
	w.restored()
}

// OnStaged's error leaves the restore staged, nothing swapped, so the drill can discard it.
func TestOnStagedFailureLeavesTheRestoreStaged(t *testing.T) {
	w := newWorld(t)
	w.failAt("onstaged")
	_, err := hooked(w, true, false)
	errWith(t, err, "restore not done, staged", "injected onstaged", "--resume or --discard")
	w.staged()
	if slices.Contains(w.calls, "write restore.json=swap") {
		t.Errorf("calls = %q", w.calls)
	}
	if _, err := discard(w); err != nil {
		t.Fatal(err)
	}
	w.unstaged()
}

// AfterSwap's error is the drill's forced failure: RS11 reverts, the live database is back and
// serving, the staged copy is kept at staged, and a discard then removes it.
func TestAfterSwapFailureReverts(t *testing.T) {
	w := newWorld(t)
	w.failAt("afterswap")
	_, err := hooked(w, false, true)
	errWith(t, err, "restore not done, reverted", "injected afterswap")
	w.staged()
	if w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		t.Error("the live team does not serve after the revert")
	}
	if _, err := discard(w); err != nil {
		t.Fatal(err)
	}
	w.unstaged()
}
