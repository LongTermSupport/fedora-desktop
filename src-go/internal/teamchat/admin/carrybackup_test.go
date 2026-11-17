package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SPEC-carry section 8 (SH-T1 to SH-T3): the backup timer's `backup --all` and a team the carry
// has touched.

// carriedTeam is team name as a carry leaves it: its team.json and the carry record of path
// (sealed.json after a seal, carried.json after an import), and no stamp.
func (w *world) carriedTeam(t *testing.T, name string, record func(paths.TeamID) string) {
	t.Helper()
	w.stampTeam(t, name)
	team := teamID(t, name)
	if err := os.Remove(w.root + paths.StampFile(team)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(w.root+record(team)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+record(team), []byte(`{"a":"carry record"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// SH-T1, SH-T3: `backup --all` backs up only stamped teams, so a sealed team and an imported team
// before its apply are skipped; `backup --team` of the sealed team is SG1's refusal (70), with
// nothing taken.
func TestBackupAllSkipsSealedAndImportedTeams(t *testing.T) {
	w, u := backupWorld(t)
	w.carriedTeam(t, "ops", paths.SealRecord)
	w.carriedTeam(t, "beta", paths.CarriedRecord)
	out := w.backupVerb(w.backupDeps(u), "backup", "--all")
	if out.code != term.ExitOK || out.stdout != "" || out.stderr != "alpha backup 1\n" {
		t.Fatalf("backup --all = %+v", out)
	}
	out = w.backupVerb(w.backupDeps(u), "backup", "--team", "ops")
	if out.code != ExitRefused || out.stdout != "" || !strings.Contains(out.stderr, carry.RecordSeal.String()) || len(u.kills) != 1 {
		t.Fatalf("backup --team ops = %+v, signals %q", out, u.kills)
	}
}

// SH-T2: while a drill holds the team lock, the timer's take of that team is busy (75's
// condition inside --all): the team's line says so, the run ends 1, and no signal is sent.
func TestBackupAllFindsADrillBusy(t *testing.T) {
	w, u := backupWorld(t)
	lock, err := teamlock.Take(t.Context(), w.root+paths.AdminRunDir, alpha(t), "team.export", 0, w, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	out := w.backupVerb(w.backupDeps(u), "backup", "--all")
	if out.code != term.ExitFailure || out.stdout != "" || !strings.HasPrefix(out.stderr, "alpha failed: ") ||
		!strings.Contains(out.stderr, "team.export") || len(u.kills) != 0 {
		t.Fatalf("backup --all under a drill = %+v, signals %q", out, u.kills)
	}
}
