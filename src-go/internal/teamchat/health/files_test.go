package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// Interface checks: the adapters are the seams the wiring hands on.
var (
	_ Teams       = Stamps{}
	_ Backups     = NoBackups{}
	_ Guests      = GuestsNotBuilt{}
	_ host.Status = StatusFiles{}
)

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteFile(path, []byte(content), 0o600, -1); err != nil {
		t.Fatal(err)
	}
}

func stampDoc(team string, appliedMS string) string {
	digest := strings.Repeat("a", 64)
	return `{"v":1,"team":"` + team + `","config":"` + digest + `","software":"abc","tuwunel":"1.9.4","applied_ms":` + appliedMS + `}`
}

// installTeam writes a stamped team under root: its stamp and its team.json named name.
func installTeam(t *testing.T, root, team, name string) {
	t.Helper()
	doc := `{"team":{"team":"` + team + `","state":"present","name":"` + name + `","port":8448,"description":"d",` +
		`"listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],"humans":["alice"],` +
		`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},` +
		`"lxc_members":[],"address":"192.0.2.10"}`
	in, err := declare.DecodeInput([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(root, paths.TeamDeclaration(id(t, team))), string(in.Team.Canonical()))
	writeAt(t, filepath.Join(root, paths.StampFile(id(t, team))), stampDoc(team, "42"))
}

func teamsDir(root string) string { return filepath.Join(root, paths.StatusRoot, "teams") }

// HR2: every stamped team, its name from team.json, its origin from the migration's state.
func TestStampsListsTheStampedTeams(t *testing.T) {
	root := t.TempDir()
	installTeam(t, root, "alpha", "Alpha Team")
	installTeam(t, root, "beta", "Beta")
	writeAt(t, filepath.Join(root, paths.MigrationDir(id(t, "beta")), "state.json"), `{}`)
	// Not teams: a name that is not a team id, a plain file, a link, a directory with no stamp.
	writeAt(t, filepath.Join(teamsDir(root), "Not_A_Team", "stamp.json"), stampDoc("x", "1"))
	writeAt(t, filepath.Join(teamsDir(root), "gamma"), "a file")
	if err := os.Symlink(filepath.Join(teamsDir(root), "alpha"), filepath.Join(teamsDir(root), "delta")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(teamsDir(root), "epsilon"), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := Stamps{Root: root}.Stamped(context.Background())
	if err != nil {
		t.Fatalf("Stamped: %v", err)
	}
	want := []Team{
		{ID: id(t, "alpha"), Name: "Alpha Team", Origin: status.OriginV2, AppliedMS: 42},
		{ID: id(t, "beta"), Name: "Beta", Origin: status.OriginMigration, AppliedMS: 42},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Stamped = %+v", got)
	}
}

func TestStampsWithNoTeamsDirectoryIsNone(t *testing.T) {
	got, err := Stamps{Root: t.TempDir()}.Stamped(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("Stamped = %+v, %v", got, err)
	}
}

func TestStampsRefusesWhatItCannotTrust(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"a broken stamp": func(t *testing.T, root string) {
			t.Helper()
			writeAt(t, filepath.Join(root, paths.StampFile(id(t, "alpha"))), `{`)
		},
		"no team.json": func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, paths.TeamDeclaration(id(t, "alpha")))); err != nil {
				t.Fatal(err)
			}
		},
		"a broken team.json": func(t *testing.T, root string) {
			t.Helper()
			writeAt(t, filepath.Join(root, paths.TeamDeclaration(id(t, "alpha"))), `{"v":1}`)
		},
		"another team's team.json": func(t *testing.T, root string) {
			t.Helper()
			installTeam(t, root, "beta", "Beta")
			raw, err := os.ReadFile(filepath.Clean(filepath.Join(root, paths.TeamDeclaration(id(t, "beta")))))
			if err != nil {
				t.Fatal(err)
			}
			writeAt(t, filepath.Join(root, paths.TeamDeclaration(id(t, "alpha"))), string(raw))
		},
		"a migration state that is a directory": func(t *testing.T, root string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(root, paths.MigrationDir(id(t, "alpha")), "state.json"), 0o750); err != nil {
				t.Fatal(err)
			}
		},
		"a teams path that is a file": func(t *testing.T, root string) {
			t.Helper()
			if err := os.RemoveAll(teamsDir(root)); err != nil {
				t.Fatal(err)
			}
			writeAt(t, teamsDir(root), "a file")
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			installTeam(t, root, "alpha", "Alpha")
			breakIt(t, root)
			if got, err := (Stamps{Root: root}).Stamped(context.Background()); err == nil {
				t.Fatalf("Stamped = %+v", got)
			}
		})
	}
}

func TestStampsStamp(t *testing.T) {
	root := t.TempDir()
	installTeam(t, root, "alpha", "Alpha")
	s := Stamps{Root: root}
	if ok, err := s.Stamp(context.Background(), id(t, "alpha")); !ok || err != nil {
		t.Fatalf("Stamp(alpha) = %v, %v", ok, err)
	}
	if ok, err := s.Stamp(context.Background(), id(t, "beta")); ok || err != nil {
		t.Fatalf("Stamp(beta) = %v, %v", ok, err)
	}
	writeAt(t, filepath.Join(root, paths.StampFile(id(t, "alpha"))), `{`)
	if _, err := s.Stamp(context.Background(), id(t, "alpha")); err == nil {
		t.Fatal("a broken stamp is a stamp")
	}
}

// statusWorld is a tick over a host-shaped tree under root, with the production stamps.
func statusWorld(t *testing.T) (*world, StatusFiles) {
	t.Helper()
	w := newWorld(t)
	root := w.dir
	installTeam(t, root, "alpha", "Alpha")
	if err := host.MkdirStatus(root, id(t, "alpha")); err != nil {
		t.Fatal(err)
	}
	tick, err := New(Config{
		Clock: w.clock, Teams: Stamps{Root: root}, Units: w.units, Prober: w.prober, Viewer: w.viewer,
		Backups: w.backups, Space: w.space, Guests: w.guests, Announcer: w.announcer, Locks: w.locks,
		StatusPath:    func(team paths.TeamID) string { return root + paths.StatusFile(team) },
		LXCOnlinePath: func(team paths.TeamID) string { return root + paths.LXCOnlineFile(team) },
		GID:           -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, StatusFiles{Root: root, Tick: tick}
}

// HR1, n2f-H1: StatusFiles.Refresh is the tick's Refresh, first or not.
func TestStatusFilesRefresh(t *testing.T) {
	w, s := statusWorld(t)
	for _, first := range []bool{true, false} {
		if err := s.Refresh(context.Background(), id(t, "alpha"), first); err != nil {
			t.Fatalf("Refresh(first %v): %v", first, err)
		}
	}
	rec, found, err := status.Read(w.dir+paths.StatusFile(id(t, "alpha")), id(t, "alpha"))
	if err != nil || !found || rec.Seq != 2 || rec.Origin != status.OriginV2 || rec.Name != "Alpha" {
		t.Fatalf("record %+v, %v, %v", rec, found, err)
	}
	if err := s.Refresh(context.Background(), id(t, "beta"), true); err == nil {
		t.Fatal("an unstamped team was refreshed")
	}
}

// HR3: Remove deletes both files and says so; with no record it says nothing.
func TestStatusFilesRemove(t *testing.T) {
	w, s := statusWorld(t)
	team := id(t, "alpha")
	if err := s.Refresh(context.Background(), team, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.dir + paths.LXCOnlineFile(team)); err != nil {
		t.Fatalf("no lxc-online.json to remove: %v", err)
	}
	line, err := s.Remove(context.Background(), team)
	if err != nil || line != "status record removed" {
		t.Fatalf("Remove = %q, %v", line, err)
	}
	for _, p := range []string{paths.StatusFile(team), paths.LXCOnlineFile(team)} {
		if _, err := os.Stat(w.dir + p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is still there: %v", p, err)
		}
	}
	if line, err := s.Remove(context.Background(), team); err != nil || line != "" {
		t.Fatalf("Remove again = %q, %v", line, err)
	}
}

// HR3: a migration-origin record is refused and kept, and so is one that cannot be read.
func TestStatusFilesRemoveRefuses(t *testing.T) {
	w, s := statusWorld(t)
	team := id(t, "alpha")
	writeAt(t, filepath.Join(w.dir, paths.MigrationDir(team), "state.json"), `{}`)
	if err := s.Refresh(context.Background(), team, true); err != nil {
		t.Fatal(err)
	}
	line, err := s.Remove(context.Background(), team)
	if err == nil || line != "" || !strings.Contains(err.Error(), "migration") {
		t.Fatalf("Remove of a migration record = %q, %v", line, err)
	}
	if _, err := os.Stat(w.dir + paths.StatusFile(team)); err != nil {
		t.Fatalf("the migration record is gone: %v", err)
	}
	writeAt(t, w.dir+paths.StatusFile(team), `{`)
	if _, err := s.Remove(context.Background(), team); err == nil {
		t.Fatal("an unreadable record was removed")
	}
}

func TestStatusFilesRemoveFailsOnAFileItCannotDelete(t *testing.T) {
	w, s := statusWorld(t)
	team := id(t, "alpha")
	if err := os.MkdirAll(filepath.Join(w.dir+paths.LXCOnlineFile(team), "inside"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(context.Background(), team); err == nil {
		t.Fatal("a directory in lxc-online.json's place was removed silently")
	}
}

// HR4: no backup and a last run of none, so the record says the backup has not run.
func TestNoBackups(t *testing.T) {
	state, err := NoBackups{}.State(context.Background(), id(t, "alpha"))
	if err != nil || state.Newest != nil || state.LastRun != status.RunNone {
		t.Fatalf("State = %+v, %v", state, err)
	}
	w := newWorld(t, "alpha")
	tick, err := New(Config{
		Clock: w.clock, Teams: w.teams, Units: w.units, Prober: w.prober, Viewer: w.viewer,
		Backups: NoBackups{}, Space: w.space, Guests: w.guests, Announcer: w.announcer, Locks: w.locks,
		StatusPath:    func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "status.json") },
		LXCOnlinePath: func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "lxc-online.json") },
		GID:           -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := tick.RunAll(context.Background()); err != nil || out[0].Result != ResultWritten {
		t.Fatalf("RunAll = %+v, %v", out, err)
	}
	rec := w.record(t, "alpha")
	if rec.Backup.LastRun != status.RunNone || !hasProblem(rec, "the backup has not run since the team was applied") {
		t.Fatalf("backup %+v problems %v", rec.Backup, whats(rec.Problems))
	}
}

// HR5: every guest probe fails; the tick records the member offline with the probe's problem,
// and asks for no notice.
func TestGuestsNotBuilt(t *testing.T) {
	_, err := GuestsNotBuilt{}.Probe(context.Background(), id(t, "alpha"), lxcA)
	if !errors.Is(err, ErrGuestProbeNotBuilt) || err.Error() != "the LXC guest probe is not built yet" {
		t.Fatalf("Probe = %v", err)
	}
	w := newWorld(t, "alpha")
	tick, err := New(Config{
		Clock: w.clock, Teams: w.teams, Units: w.units, Prober: w.prober, Viewer: w.viewer,
		Backups: w.backups, Space: w.space, Guests: GuestsNotBuilt{}, Announcer: w.announcer, Locks: w.locks,
		StatusPath:    func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "status.json") },
		LXCOnlinePath: func(team paths.TeamID) string { return filepath.Join(w.dir, team.String(), "lxc-online.json") },
		GID:           -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := tick.RunAll(context.Background()); err != nil || out[0].Result != ResultWritten {
		t.Fatalf("RunAll = %+v, %v", out, err)
	}
	rec := w.record(t, "alpha")
	if rec.Members[1].Online == nil || *rec.Members[1].Online || !hasProblem(rec, "the seat of "+lxcA+" could not be probed") {
		t.Fatalf("members %+v problems %v", rec.Members, whats(rec.Problems))
	}
	if len(w.announcer.asked) != 0 {
		t.Fatalf("asked %+v", w.announcer.asked)
	}
}
