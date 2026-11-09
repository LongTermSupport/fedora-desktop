package restore

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func validate(w *world, req Request) (*Validated, error) {
	return Validate(context.Background(), w.deps(), req)
}

// refusedWith asserts err is a refusal (exit 70) naming every want.
func refusedWith(t *testing.T, err error, want ...string) {
	t.Helper()
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	for _, s := range want {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the refusal %q does not name %q", err, s)
		}
	}
}

// A team that passes every rule is validated: the files, the tar and the merge.
func TestValidateAcceptsAnAppliedServingTeam(t *testing.T) {
	w := newWorld(t)
	v, err := validate(w, w.request())
	if err != nil {
		t.Fatal(err)
	}
	if v.Merged.Roster.RestoreEpoch != 3 || v.Merged.Roster.Rev != 6 || !slices.Equal(v.Merged.Lists.Reissue, []string{h2}) {
		t.Errorf("merged = %+v", v.Merged)
	}
	if v.Team.ID != w.team || string(v.TeamFile) != string(w.decl.Canonical()) || v.Tar.Token != "tok" || !strings.Contains(string(v.Config), "database_path") {
		t.Errorf("validated = %+v", v)
	}
	if len(w.callsSince(0)) != 0 || w.index("serve") != -1 || w.has(paths.RestoreRecord(w.team)) || w.floor() != (EpochFloor{}) {
		t.Errorf("validation took effects: %v", w.calls)
	}
}

// RV1 names the record's way out, whatever its step.
func TestValidateRefusesARecordedRestore(t *testing.T) {
	w := newWorld(t)
	w.put(paths.RestoreRecord(w.team), string(EncodeRecord(Record{Team: w.team, Backup: 3, Step: StepStaged, Roster: w.current})))
	_, err := validate(w, w.request())
	refusedWith(t, err, "--discard")
	if w.record().Step != StepStaged {
		t.Error("the record was changed")
	}
}

// RV1: no record on a fresh restore. RV2: no leftover, each named.
func TestValidateRefusesARecordAndEveryLeftover(t *testing.T) {
	for name, c := range map[string]struct {
		plant func(w *world)
		names string
	}{
		"record":         {func(w *world) { w.put(paths.RestoreRecord(w.team), "{}") }, "restore --team ops --resume"},
		"db.restore":     {func(w *world) { w.mark("db.restore-3", "x") }, "db.restore-3"},
		"db.previous":    {func(w *world) { w.mark("db.previous", "x") }, "db.previous"},
		"restore config": {func(w *world) { w.put(paths.RestoreConfig(w.team), "x") }, "config.restore.toml"},
		"block":          {func(w *world) { w.put(paths.RestoreBlock(w.team), "") }, "restore.block"},
		"ctl previous":   {func(w *world) { w.put(paths.RosterFile(w.team)+".previous", "") }, "roster.json.previous"},
		"staging unit":   {func(w *world) { w.unit(paths.RestoreUnit(w.team)).st.Load = "loaded" }, paths.RestoreUnit(opsTeam(t))},
		"secure unit":    {func(w *world) { w.unit(paths.RestoreCtlUnit(w.team)).st.Load = "loaded" }, paths.RestoreCtlUnit(opsTeam(t))},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.plant(w)
			_, err := validate(w, w.request())
			refusedWith(t, err, c.names)
		})
	}
}

// RV3: the team is applied and not of migration origin.
func TestValidateRefusesATeamNotApplied(t *testing.T) {
	for name, c := range map[string]struct {
		change func(w *world)
		names  string
	}{
		"no stamp":       {func(w *world) { removeFile(w, paths.StampFile(w.team)) }, "team apply"},
		"no team.json":   {func(w *world) { removeFile(w, paths.TeamDeclaration(w.team)) }, "team.json"},
		"no server-name": {func(w *world) { removeFile(w, paths.ServerNameFile(w.team)) }, "server-name"},
		"migration":      {func(w *world) { w.statusRec.Origin = "migration" }, "migrated"},
		"bad team.json":  {func(w *world) { w.put(paths.TeamDeclaration(w.team), "{}") }, "team.json"},
		"another team id": {func(w *world) {
			w.put(paths.TeamDeclaration(w.team), strings.Replace(w.read(paths.TeamDeclaration(w.team)), `"ops"`, `"web"`, 1))
		}, "team.json"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.change(w)
			_, err := validate(w, w.request())
			if err == nil || !strings.Contains(err.Error(), c.names) {
				t.Errorf("err = %v, want one naming %q", err, c.names)
			}
		})
	}
}

func removeFile(w *world, path string) {
	w.t.Helper()
	if err := w.os.Remove(path); err != nil {
		w.t.Fatal(err)
	}
}

// RV4: the tar validates and is this server's; a fresh restore needs meta/N and N at or above
// min_backup.
func TestValidateTheBackup(t *testing.T) {
	w := newWorld(t)
	w.tarErr = errors.New("a member is a link")
	if _, err := validate(w, w.request()); err == nil || !strings.Contains(err.Error(), "a member is a link") {
		t.Errorf("a bad tar = %v", err)
	}
	w = newWorld(t)
	w.tarServer = "other.example"
	_, err := validate(w, w.request())
	refusedWith(t, err, "server_name")
	w = newWorld(t)
	removeFile(w, paths.HomeserverPrivateBackupMeta(w.team)+"/7")
	_, err = validate(w, w.request())
	refusedWith(t, err, "meta/7")
	w = newWorld(t)
	w.put(paths.RestoreEpochFile(w.team), string(EncodeEpochFloor(EpochFloor{Epoch: 4, MinBackup: 8})))
	_, err = validate(w, w.request())
	refusedWith(t, err, "--roster-from-backup", "8")
	w = newWorld(t)
	if _, err := validate(w, Request{Team: w.team}); err == nil {
		t.Error("backup 0 was accepted")
	}
}

// RM10's H: the merge raises above restore-epoch.json's epoch; a bad file fails.
func TestValidateMergesAboveTheHighWater(t *testing.T) {
	w := newWorld(t)
	w.put(paths.RestoreEpochFile(w.team), string(EncodeEpochFloor(EpochFloor{Epoch: 40})))
	v, err := validate(w, w.request())
	if err != nil || v.Merged.Roster.RestoreEpoch != 41 {
		t.Fatalf("merged above 40 = %v, %v", v, err)
	}
	w.put(paths.RestoreEpochFile(w.team), "{")
	if _, err := validate(w, w.request()); err == nil {
		t.Error("a bad restore-epoch.json was accepted")
	}
}

// RV5: a pending operation in either roster is refused, naming the verb; under --offline the
// current one is resolved instead.
func TestValidatePending(t *testing.T) {
	w := newWorld(t)
	w.current.Pending = &apply.Pending{Op: apply.OpMemberRetire, Step: "x", Handle: h1}
	w.put(paths.RosterFile(w.team), string(w.current.Encode()))
	_, err := validate(w, w.request())
	refusedWith(t, err, "member retire --team ops --handle "+h1)
	w.answers = -1
	w.unit(paths.ServerUnit(w.team)).st.Active = "failed"
	v, err := validate(w, Request{Team: w.team, Backup: backupN, Offline: true})
	if err != nil || v.Merged.Lists.Pending == "" {
		t.Errorf("offline = %v, %v", v, err)
	}
	w = newWorld(t)
	w.backup.Pending = &apply.Pending{Op: apply.OpTeamApply, Step: "x"}
	_, err = validate(w, w.request())
	refusedWith(t, err, "another backup")
	w = newWorld(t)
	w.put(paths.RosterFile(w.team), "{")
	if _, err := validate(w, w.request()); err == nil {
		t.Error("an unreadable current roster was accepted without --roster-from-backup")
	}
}

// RM11: --roster-from-backup never reads the current roster.
func TestValidateFromBackup(t *testing.T) {
	w := newWorld(t)
	w.put(paths.RosterFile(w.team), "{")
	v, err := validate(w, Request{Team: w.team, Backup: backupN, FromBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if e := v.Merged.Roster.RestoreEpoch; e <= 2 || e > 2+fromBackupWindow || !slices.Equal(v.Merged.Lists.Revoke, []string{h1, h2}) {
		t.Errorf("merged = %+v", v.Merged)
	}
	w.statusRec = nil
	if _, err := validate(w, Request{Team: w.team, Backup: backupN, FromBackup: true}); err == nil {
		t.Error("a --roster-from-backup restore with no status record was accepted")
	}
}

// RV6: the approval holds the applied team.json.
func TestValidateRefusesAnApprovalNotApplied(t *testing.T) {
	w := newWorld(t)
	changed := w.decl
	changed.Humans = []string{"alice", "bob"}
	w.put(paths.TeamDeclaration(w.team), string(changed.Canonical()))
	_, err := validate(w, w.request())
	refusedWith(t, err, "an approval is not applied: run team apply first")
}

// RV7: a stopped, healthy unit is started first; online, the server must answer.
func TestValidateStartsAStoppedTeam(t *testing.T) {
	w := newWorld(t)
	w.unit(paths.ServerUnit(w.team)).st.Active = "inactive"
	if _, err := validate(w, w.request()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.calls, []string{"serve"}) {
		t.Errorf("calls = %v, want the one start", w.calls)
	}
	w = newWorld(t)
	w.unit(paths.ServerUnit(w.team)).st.Active = "inactive"
	_, err := validate(w, Request{Team: w.team, Backup: backupN, Offline: true})
	refusedWith(t, err, "the homeserver serves: restore without --offline")
	if w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		t.Error("the started team was not left serving")
	}
}

func TestValidateRefusesAServerThatDoesNotAnswer(t *testing.T) {
	w := newWorld(t)
	w.answers = -1
	_, err := validate(w, w.request())
	refusedWith(t, err, "--offline", "every event after", "no pre-restore backup")
	w = newWorld(t)
	live := w.unit(paths.ServerUnit(w.team))
	live.st.Active = "inactive"
	w.crashLoop = true
	_, err = validate(w, w.request())
	refusedWith(t, err, "--offline", "did not start")
	w = newWorld(t)
	w.unit(paths.ServerUnit(w.team)).st.Active = "failed"
	_, err = validate(w, w.request())
	refusedWith(t, err, "--offline")
	if slices.Contains(w.calls, "serve") {
		t.Error("a failed unit was started")
	}
}

// RO1: an offline restore is refused when any probe within 30 s answers.
func TestValidateOfflineProbes(t *testing.T) {
	w := newWorld(t)
	w.answers = 2
	_, err := validate(w, Request{Team: w.team, Backup: backupN, Offline: true})
	refusedWith(t, err, "the homeserver serves: restore without --offline")
	if w.probes != 3 {
		t.Errorf("probes = %d, want the third to answer", w.probes)
	}
	w = newWorld(t)
	w.answers = -1
	if _, err := validate(w, Request{Team: w.team, Backup: backupN, Offline: true}); err != nil {
		t.Fatal(err)
	}
	if w.probes != offlineProbes || w.sleeps != offlineProbes-1 {
		t.Errorf("probes = %d, sleeps = %d", w.probes, w.sleeps)
	}
}
