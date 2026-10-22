package health

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

type restores struct {
	state RestoreState
	err   error
}

func (r *restores) State(context.Context, paths.TeamID) (RestoreState, error) {
	return r.state, r.err
}

func interrupted() RestoreState {
	return RestoreState{
		Restore: &status.Restore{Step: "swap", Backup: 7, Pre: 9},
		Problem: status.Problem{What: "restore interrupted", Fix: "teamchat-admin restore --team alpha --resume"},
	}
}

// HB2: a recorded restore is copied into the record with its problem.
func TestARecordedRestoreIsOnTheRecord(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.state = interrupted()
	if out := w.run(t); out[0].Result != ResultWritten {
		t.Fatalf("outcome %+v", out[0])
	}
	rec := w.record(t, "alpha")
	if rec.Restore == nil || *rec.Restore != (status.Restore{Step: "swap", Backup: 7, Pre: 9}) {
		t.Fatalf("restore %+v", rec.Restore)
	}
	if !slices.Contains(rec.Problems, interrupted().Problem) {
		t.Fatalf("problems %v", rec.Problems)
	}
}

// HB2: with no restore.json the field is null, a restore the last record carried included.
func TestNoRecordedRestoreClearsTheField(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.state = interrupted()
	w.run(t)
	w.restores.state = RestoreState{}
	w.run(t)
	rec := w.record(t, "alpha")
	if rec.Restore != nil || hasProblem(rec, "restore") {
		t.Fatalf("restore %+v, problems %v", rec.Restore, whats(rec.Problems))
	}
}

// HB3: three missed probes restart no team that has a restore.json; the miss and the problem
// stand, with no re-probe.
func TestNoRestartWhileARestoreIsRecorded(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.state = interrupted()
	w.prober.fails = 3
	if out := w.run(t); out[0].Result != ResultWritten {
		t.Fatalf("outcome %+v", out[0])
	}
	rec := w.record(t, "alpha")
	if w.units.restarts != 0 || len(w.clock.sleeps) != 0 || rec.Homeserver.Probe != status.ProbeMissed {
		t.Fatalf("restarts %d, sleeps %v, probe %s", w.units.restarts, w.clock.sleeps, rec.Homeserver.Probe)
	}
	if !hasProblem(rec, "restore interrupted") {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

// HB3: a record that cannot be read fails the team's tick before the probe, so nothing is
// restarted on a guess.
func TestAnUnreadableRestoreFailsTheTeam(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.err = errors.New("restore.json: not a regular file")
	w.prober.fails = 3
	out := w.run(t)
	if out[0].Result != ResultFailed || !strings.Contains(out[0].Err.Error(), "restore.json: not a regular file") ||
		!strings.Contains(out[0].Err.Error(), "the restore record of alpha") {
		t.Fatalf("outcome %+v", out[0])
	}
	if w.units.restarts != 0 || w.prober.calls != 0 {
		t.Fatalf("restarts %d, probes %d", w.units.restarts, w.prober.calls)
	}
}

// HB2: a refresh under a caller's lock copies the restore too.
func TestARefreshCopiesTheRestore(t *testing.T) {
	w := newWorld(t, "alpha")
	w.restores.state = interrupted()
	if err := w.tick(t).Refresh(context.Background(), id(t, "alpha")); err != nil {
		t.Fatal(err)
	}
	if rec := w.record(t, "alpha"); rec.Restore == nil || rec.Restore.Step != "swap" {
		t.Fatalf("restore %+v", rec.Restore)
	}
}

func TestNewRefusesAConfigWithoutRestores(t *testing.T) {
	w := newWorld(t, "alpha")
	_, err := New(Config{
		Clock: w.clock, Teams: w.teams, Units: w.units, Prober: w.prober, Viewer: w.viewer,
		Backups: w.backups, Space: w.space, Guests: w.guests, Announcer: w.announcer, Locks: w.locks,
		StatusPath:    func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "status.json") },
		LXCOnlinePath: func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "lxc-online.json") },
	})
	if err == nil {
		t.Fatal("New without Restores succeeded")
	}
}
