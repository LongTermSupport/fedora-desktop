package restore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

func run(w *world, req Request) (Answer, error) {
	return Run(context.Background(), w.deps(), nil, req)
}

func resume(w *world) (Answer, error) {
	return Resume(context.Background(), w.deps(), nil, w.team)
}

func discard(w *world) (Answer, error) {
	return Discard(context.Background(), w.deps(), w.team)
}

// happyCalls is the whole effect log of a fresh online restore of backup 7.
func happyCalls(w *world) []string {
	stage, live := "stop "+paths.RestoreUnit(w.team), "stop "+paths.ServerUnit(w.team)
	return []string{
		"write restore.json=staging", "write config.restore.toml", "run staging restoring=true", "child secure", stage,
		"run staging restoring=false", "child check staged=true", stage, "syncfs", "write restore.json=staged",
		"backup", "write restore.json=staged", "child export", "write restore.json=staged",
		"write restore-epoch.json", "write restore.json=swap", "write restore.block", live,
		"rename db db.previous", "rename db.restore-7 db", "fsync", "child swap", "write restore.json=start", "remove restore.block",
		"serve", "child check staged=false", "bootstrap",
		"write restore.json=clean", "removetree db.previous", "remove config.restore.toml", "child finish", "remove restore.json",
		"status",
	}
}

// offlineCalls is happyCalls without RS4 and RS5.
func offlineCalls(w *world) []string {
	calls := happyCalls(w)
	i := slices.Index(calls, "backup")
	return slices.Delete(calls, i, i+4)
}

// fromBackupCalls is happyCalls without RS4: a --roster-from-backup restore takes no backup.
func fromBackupCalls(w *world) []string {
	return slices.DeleteFunc(happyCalls(w), func(c string) bool { return c == "backup" })
}

// offlineWorld is a team whose homeserver crash-loops and never answers (RO7).
func offlineWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.crashLoop, w.answers = true, -1
	w.unit(paths.ServerUnit(w.team)).st.Active = "activating"
	return w
}

func (w *world) offline() Request {
	return Request{Team: w.team, Backup: backupN, Offline: true}
}

// restored asserts the team serves the restored copy and nothing of the restore is left.
func (w *world) restored() {
	w.t.Helper()
	if w.db() != "restored" {
		w.t.Errorf("db holds %q, want the restored copy", w.db())
	}
	if st := w.unit(paths.ServerUnit(w.team)).st; st.Active != "active" {
		w.t.Errorf("the homeserver is %s", st.Active)
	}
	for _, name := range []string{namePrevious, restoreName(w.team, backupN)} {
		if exists(w.t, w.state+"/"+name) {
			w.t.Errorf("%s is left", name)
		}
	}
	for _, path := range []string{paths.RestoreRecord(w.team), paths.RestoreBlock(w.team), paths.RestoreConfig(w.team)} {
		if w.has(path) {
			w.t.Errorf("%s is left", path)
		}
	}
}

// staged asserts the restore is recorded at staged and the old database is in place.
func (w *world) staged() Record {
	w.t.Helper()
	r := w.record()
	if r.Step != StepStaged || w.db() != "live" || marker(w.t, w.state, restoreName(w.team, backupN)) != "restored" ||
		w.has(paths.RestoreBlock(w.team)) {
		w.t.Errorf("step %s, db %q, block %v: want staged over the old database", r.Step, w.db(), w.has(paths.RestoreBlock(w.team)))
	}
	return r
}

// unstaged asserts RS10 left nothing: no record, config or copy, the old database in place.
func (w *world) unstaged() {
	w.t.Helper()
	if w.has(paths.RestoreRecord(w.team)) || w.has(paths.RestoreConfig(w.team)) || exists(w.t, w.state+"/"+restoreName(w.team, backupN)) ||
		w.db() != "live" {
		w.t.Errorf("RS10 left the restore: calls %q", w.calls)
	}
}

// decoded is an answer's JSON as a map.
func decoded(t *testing.T, a Answer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(a.Encode(), &m); err != nil {
		t.Fatalf("the answer %s is not JSON: %v", a.Encode(), err)
	}
	return m
}

// object is m[key] as an object.
func object(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	o, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is %v, not an object", key, m[key])
	}
	return o
}

// errWith asserts err names every want.
func errWith(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one naming %q", want)
	}
	for _, s := range want {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the error %q does not name %q", err, s)
		}
	}
}

func (w *world) failAt(name string) {
	w.fail[name] = errors.New("injected " + name)
}

// at is the kill number of the effect name, the first time it comes.
func at(w *world, name string) int {
	w.t.Helper()
	i := slices.Index(happyCalls(w), name)
	if i < 0 {
		w.t.Fatalf("no effect %q", name)
	}
	return i + 1
}

// killRun runs a fresh restore and asserts it was killed.
func killRun(t *testing.T, w *world, req Request) {
	t.Helper()
	var err error
	if inGoroutine(func() { _, err = run(w, req) }) {
		t.Fatalf("the run was not killed: %v", err)
	}
}

// The happy online restore: every effect in order (RS3's verify before syncfs before staged,
// RS6's epoch before swap and fsync before start), the end state and the event.
func TestRunRestoresOnline(t *testing.T) {
	w := newWorld(t)
	w.unloadAfter = 4
	a, err := run(w, w.request())
	if err != nil {
		t.Fatal(err)
	}
	if want := happyCalls(w); !slices.Equal(w.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", w.calls, want)
	}
	w.restored()
	if w.floor() != (EpochFloor{Epoch: 3}) {
		t.Errorf("floor = %+v, want epoch 3", w.floor())
	}
	if w.sleeps != 2 {
		t.Errorf("sleeps = %d, want the unload wait's two", w.sleeps)
	}
	want := "restored backup 7, 3 events lost, export at " + paths.RestoreLostFile(w.team, backupN)
	if len(w.events) != 1 || !slices.Equal(w.events[0], []string{want}) {
		t.Errorf("events = %q", w.events)
	}
	if a.Printed {
		t.Error("a clean restore's answer is Printed")
	}
}

// RS9's online answer.
func TestRunAnswersOnline(t *testing.T) {
	w := newWorld(t)
	a, err := run(w, w.request())
	if err != nil {
		t.Fatal(err)
	}
	m := decoded(t, a)
	lost := object(t, m, "lost")
	if m["team"] != "ops" || m["backup"] != float64(backupN) || m["pre"] != float64(preM) || m["offline"] != false || m["pending"] != nil {
		t.Errorf("answer = %s", a.Encode())
	}
	if lost["count"] != float64(3) || lost["first_ms"] != float64(backupMS+10) || lost["end_ms"] != float64(backupMS+50) ||
		lost["truncated"] != false || lost["path"] != paths.RestoreLostFile(w.team, backupN) {
		t.Errorf("lost = %v", lost)
	}
	raw := string(a.Encode())
	for _, s := range []string{
		`"reissue":["` + h2 + `"]`, `"return":[]`, `"unknown":[]`, NoteElementPasswords,
		"between the export's end (1700000000050) and the stop are lost", `"changed":["CHANGED secured","CHANGED swapped","CHANGED bootstrapped"]`,
	} {
		if !strings.Contains(raw, s) {
			t.Errorf("the answer %s lacks %s", raw, s)
		}
	}
}

// The offline restore (RO3, RO5, RO7): no backup, no export, a crash-looping unit that RS18
// sees failed, and the loss stated.
func TestRunRestoresOffline(t *testing.T) {
	w := offlineWorld(t)
	a, err := run(w, w.offline())
	if err != nil {
		t.Fatal(err)
	}
	if want := offlineCalls(w); !slices.Equal(w.calls, want) {
		t.Errorf("calls =\n%q\nwant\n%q", w.calls, want)
	}
	w.restored()
	m := decoded(t, a)
	lost := object(t, m, "lost")
	if m["offline"] != true || m["pre"] != nil || lost["exported"] != false || lost["since_ms"] != float64(backupMS) {
		t.Errorf("answer = %s", a.Encode())
	}
	line := "offline restore: every event after 2023-11-14T22:13:20Z is lost and was not exported; no pre-restore backup " +
		"was taken; the database that would not start was removed"
	if !strings.Contains(string(a.Encode()), line) || len(w.events) != 1 || w.events[0][0] != line {
		t.Errorf("answer = %s, events = %q", a.Encode(), w.events)
	}
}

// A refusal of validation takes no effect and stays a refusal.
func TestRunRefusesWhatValidationRefuses(t *testing.T) {
	w := newWorld(t)
	w.tarServer = "other.example"
	_, err := run(w, w.request())
	refusedWith(t, err, "server_name")
	if len(w.calls) != 0 {
		t.Errorf("calls = %v", w.calls)
	}
}

// RS3: a verify that answers the backup's epoch fails step 05, before syncfs and staged, and
// RS10 cleans up.
func TestRunVerifyMustAnswerTheMergedEpoch(t *testing.T) {
	w := newWorld(t)
	w.stagedEpoch = &Epoch{RestoreEpoch: 2, Rev: 5}
	_, err := run(w, w.request())
	errWith(t, err, "restore not done", "restore epoch 2 rev 5", "merged epoch 3 rev 6")
	if slices.Contains(w.calls, "syncfs") || slices.Contains(w.calls, "write restore.json=staged") {
		t.Errorf("calls = %q", w.calls)
	}
	w.unstaged()
}

// RS3: the staging unit must be the same run after secure and after the verify's check.
func TestRunStagingUnitMustNotRestart(t *testing.T) {
	for name, c := range map[string]struct {
		effect string
		change func(u *UnitState)
	}{
		"new invocation after secure": {"child secure", func(u *UnitState) { u.InvocationID = "other" }},
		"a restart after secure":      {"child secure", func(u *UnitState) { u.NRestarts = 1 }},
		"new invocation after check":  {"child check staged=true", func(u *UnitState) { u.InvocationID = "other" }},
		"failed after check":          {"child check staged=true", func(u *UnitState) { u.Active = "failed" }},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.hooks[c.effect] = func() { c.change(&w.unit(paths.RestoreUnit(w.team)).st) }
			_, err := run(w, w.request())
			errWith(t, err, "restore not done", "is not the run it was")
			w.unstaged()
		})
	}
}

// RS3: a staging unit still loaded 10 s after its stop fails step 05.
func TestRunWaitsTenSecondsForTheUnload(t *testing.T) {
	w := newWorld(t)
	w.unloadAfter = 100
	_, err := run(w, w.request())
	errWith(t, err, "restore not done", "still loaded")
	if w.sleeps != unloadPolls || slices.Contains(w.calls, "run staging restoring=false") {
		t.Errorf("sleeps = %d, calls = %q", w.sleeps, w.calls)
	}
}

// RS2: a db.restore-7 planted after RV2 is refused before anything is started, and left for
// RV2 to name.
func TestRunRefusesAPlantBeforeTheStart(t *testing.T) {
	w := newWorld(t)
	w.hooks["write config.restore.toml"] = func() { w.mark(restoreName(w.team, backupN), "plant") }
	_, err := run(w, w.request())
	refusedWith(t, err, "restore not done", "db.restore-7 appeared after validation")
	if slices.ContainsFunc(w.calls, func(c string) bool { return strings.HasPrefix(c, "run ") || strings.HasPrefix(c, "removetree") }) {
		t.Errorf("calls = %q", w.calls)
	}
	if marker(t, w.state, restoreName(w.team, backupN)) != "plant" || w.has(paths.RestoreRecord(w.team)) || w.has(paths.RestoreConfig(w.team)) {
		t.Error("the plant was removed, or the record or config left")
	}
	delete(w.hooks, "write config.restore.toml")
	_, err = run(w, w.request())
	refusedWith(t, err, "db.restore-7")
}

// RS10: a failed clean-up leaves the record at staging with both errors; the resume finishes
// it, and a fresh run then restores.
func TestRunCleanUpFailureLeavesStaging(t *testing.T) {
	w := newWorld(t)
	w.failAt("child secure")
	w.failAt("remove config.restore.toml")
	_, err := run(w, w.request())
	errWith(t, err, "restore not done", "injected child secure", "injected remove config.restore.toml", "--resume")
	if w.record().Step != StepStaging {
		t.Fatalf("step = %s", w.record().Step)
	}
	w.fail = map[string]error{}
	_, err = resume(w)
	errWith(t, err, "restore not done", "interrupted")
	w.unstaged()
	if _, err = run(w, w.request()); err != nil {
		t.Fatal(err)
	}
	w.restored()
}

// The failures of RS1 and RU1 are "not done".
func TestRunEarlyFailures(t *testing.T) {
	for _, effect := range []string{
		"write restore.json=staging", "write config.restore.toml", "run staging restoring=true", "syncfs",
		"run staging restoring=false", "child check staged=true",
	} {
		t.Run(effect, func(t *testing.T) {
			w := newWorld(t)
			w.failAt(effect)
			_, err := run(w, w.request())
			errWith(t, err, "restore not done", "injected")
			w.unstaged()
		})
	}
}

// The staging unit and the sandbox and version it is made from must be usable.
func TestRunStagingUnitProblems(t *testing.T) {
	w := newWorld(t)
	w.hooks["run staging restoring=true"] = func() { w.unit(paths.RestoreUnit(w.team)).st.Active = "failed" }
	d := w.deps()
	d.Units = startFails{worldUnits{w}, w}
	_, err := Run(context.Background(), d, nil, w.request())
	errWith(t, err, "restore not done", "is failed after its start")
	w = newWorld(t)
	d = w.deps()
	d.Sandbox = func(paths.TeamID) []string { return []string{"Restart=always"} }
	_, err = Run(context.Background(), d, nil, w.request())
	errWith(t, err, "restore not done", "lacks DynamicUser")
	d.Version = func(paths.TeamID) (paths.ServerVersion, error) {
		return paths.ServerVersion{}, errors.New("no units.json")
	}
	_, err = Run(context.Background(), d, nil, w.request())
	errWith(t, err, "no units.json")
	d = w.deps()
	d.Self = "relative"
	_, err = Run(context.Background(), d, nil, w.request())
	errWith(t, err, "absolute")
}

// startFails is a unit manager whose staging unit is failed whenever read after a start.
type startFails struct {
	worldUnits

	w *world
}

func (s startFails) State(ctx context.Context, unit string) (UnitState, error) {
	st, err := s.worldUnits.State(ctx, unit)
	if unit == paths.RestoreUnit(s.w.team) && st.Load == "loaded" {
		st.Active = "failed"
	}
	return st, err
}

// RS2: the config must hold exactly one of each line it rewrites.
func TestRestoreConfig(t *testing.T) {
	w := newWorld(t)
	got, err := restoreConfig([]byte(w.config()), w.team, backupN)
	if err != nil {
		t.Fatal(err)
	}
	want := "database_path = \"" + paths.HomeserverRestoreDatabase(w.team, backupN) + "\"\n"
	if !strings.Contains(string(got), want) || !strings.Contains(string(got), "address = [\"127.0.0.1\", \"::1\"]\n") ||
		!strings.Contains(string(got), "database_backup_path = \""+paths.HomeserverBackups(w.team)+"\"\n") ||
		!strings.Contains(string(got), "port = 8448\n") {
		t.Errorf("config =\n%s", got)
	}
	for name, config := range map[string]string{
		"no database_path":    strings.Replace(w.config(), "database_path", "#", 1),
		"two addresses":       w.config() + "address = []\n",
		"no backup path":      strings.Replace(w.config(), "database_backup_path", "#", 1),
		"already the restore": string(got),
	} {
		if _, err := restoreConfig([]byte(config), w.team, backupN); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// RS15: a live child that ran and answered keeps its own failure; only a homeserver it could
// not reach is 69. The export (RS5) and the live check (RS7) are the two asks of the live server.
func TestLiveChildRefusalIsNotServerLost(t *testing.T) {
	refused := errors.New("the child refused")
	for _, effect := range []string{"child export", "child check staged=false"} {
		w := newWorld(t)
		w.fail[effect] = refused
		_, err := run(w, w.request())
		if !errors.Is(err, refused) || errors.Is(err, matrix.ErrUnreachable) {
			t.Errorf("%s refused: %v, want the refusal and not the homeserver lost", effect, err)
		}
		w = newWorld(t)
		w.fail[effect] = fmt.Errorf("lost: %w", matrix.ErrUnreachable)
		if _, err = run(w, w.request()); !errors.Is(err, matrix.ErrUnreachable) {
			t.Errorf("%s lost the homeserver: %v, want it lost (69)", effect, err)
		}
	}
}

// RS4 or RS5 failing leaves the record staged and the team serving; the resume takes no second
// backup, exports again, and completes.
func TestRunPreBackupAndExportFailures(t *testing.T) {
	w := newWorld(t)
	w.failAt("backup")
	_, err := run(w, w.request())
	errWith(t, err, "restore not done, staged", "pre-restore backup", "restore --team ops --resume or --discard")
	if r := w.staged(); r.Pre != 0 || w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		t.Errorf("pre = %d, or the team does not serve", r.Pre)
	}
	w = newWorld(t)
	w.fail["child export"] = fmt.Errorf("the export: %w", matrix.ErrUnreachable)
	_, err = run(w, w.request())
	if !errors.Is(err, matrix.ErrUnreachable) {
		t.Errorf("an export that lost the homeserver = %v, want it lost (69)", err)
	}
	if r := w.staged(); r.Pre != preM || r.Lost != nil {
		t.Errorf("record = %+v", r)
	}
	w.fail = map[string]error{}
	mark := len(w.calls)
	if _, err = resume(w); err != nil {
		t.Fatal(err)
	}
	if since := w.callsSince(mark); slices.Contains(since, "backup") || w.exported != 2 {
		t.Errorf("the resume ran %q, exports %d", since, w.exported)
	}
	w.restored()
}

// A pre-restore backup that answers no backup number is a failure.
func TestRunPreBackupMustAnswerANumber(t *testing.T) {
	w := newWorld(t)
	d := w.deps()
	d.Backup = func(context.Context, *teamlock.Lock, []int64) (int64, error) { return 0, nil }
	_, err := Run(context.Background(), d, nil, w.request())
	errWith(t, err, "restore not done, staged", "the number 0")
}
