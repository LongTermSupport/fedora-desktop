package health

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// HR1: Refresh writes the record under the caller's lock, so it takes none: a lock the tick
// would find busy does not stop it, and the origin is the Team's.
func TestRefreshWritesUnderTheCallersLock(t *testing.T) {
	w := newWorld(t, "alpha")
	w.teams.list[0].Origin = status.OriginMigration
	w.locks.busy[id(t, "alpha")] = true
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	rec := w.record(t, "alpha")
	if rec.Seq != 1 || rec.Origin != status.OriginMigration || rec.Name != "Alpha" || rec.Homeserver.Probe != status.ProbeOK {
		t.Fatalf("record %+v", rec)
	}
	if rec.Members[1].Online == nil || !*rec.Members[1].Online {
		t.Fatalf("members %+v", rec.Members)
	}
}

// HR1: under Refresh a missed probe past the start grace is recorded missed, with no re-probe
// and no restart.
func TestRefreshNeverRestarts(t *testing.T) {
	w := newWorld(t, "alpha")
	w.prober.fails = 3
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if w.units.restarts != 0 || w.prober.calls != 1 || len(w.clock.sleeps) != 0 {
		t.Fatalf("restarts %d probes %d sleeps %v", w.units.restarts, w.prober.calls, w.clock.sleeps)
	}
	rec := w.record(t, "alpha")
	if rec.Homeserver.Probe != status.ProbeMissed || len(rec.Events) != 0 {
		t.Fatalf("homeserver %+v events %+v", rec.Homeserver, rec.Events)
	}
}

func TestRefreshAnUnstampedTeamIsAnError(t *testing.T) {
	w := newWorld(t, "alpha")
	err := w.tick(t).Refresh(t.Context(), id(t, "beta"))
	if err == nil || !strings.Contains(err.Error(), "beta has no stamp") {
		t.Fatalf("Refresh = %v", err)
	}
	w.teams.err = errors.New("unreadable")
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("Refresh with the stamps failing = %v", err)
	}
}

// HR1, n2f-H2: another writer between the read and the write is an error, and its record stays.
func TestRefreshAnotherWriterIsAnError(t *testing.T) {
	w := newWorld(t, "alpha")
	tick := w.tick(t)
	if err := tick.Refresh(t.Context(), id(t, "alpha")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(w.dir, "alpha", "status.json")
	w.viewer.during = func() {
		rec, _, err := status.Read(path, id(t, "alpha"))
		if err != nil {
			t.Error(err)
			return
		}
		next := status.Next(&rec, rec.Team, w.clock.Now())
		next.Problems = []status.Problem{{What: "written by another", Fix: "none"}}
		if err := status.Write(path, next, -1); err != nil {
			t.Error(err)
		}
	}
	err := tick.Refresh(t.Context(), id(t, "alpha"))
	if err == nil || !strings.Contains(err.Error(), "another writer wrote the record") {
		t.Fatalf("Refresh = %v", err)
	}
	if rec := w.record(t, "alpha"); rec.Seq != 2 || !hasProblem(rec, "written by another") {
		t.Fatalf("record %+v", rec)
	}
}

func TestRefreshARemovedTeamIsAnError(t *testing.T) {
	w := newWorld(t, "alpha")
	w.teams.removed[id(t, "alpha")] = true
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("Refresh = %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "alpha", "status.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a removed team's record was written")
	}
}

func TestRefreshFailures(t *testing.T) {
	w := newWorld(t, "alpha")
	w.backups.err = errors.New("no backups")
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err == nil || !strings.Contains(err.Error(), "no backups") {
		t.Fatalf("Refresh with the backups failing = %v", err)
	}
	w.backups.err = nil
	if err := os.WriteFile(filepath.Join(w.dir, "alpha", "lxc-online.json"), []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha")); err == nil {
		t.Fatal("Refresh over a broken lxc-online.json succeeded")
	}
}
