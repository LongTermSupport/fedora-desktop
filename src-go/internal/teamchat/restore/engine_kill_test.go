package restore

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// step is the record's step, "" when there is none.
func (w *world) step() Step {
	w.t.Helper()
	if !w.has(paths.RestoreRecord(w.team)) {
		return ""
	}
	return w.record().Step
}

// invariants are SM5's after a kill: no db without the block, and a restored db with no block
// only once the record is at start or past it.
func (w *world) invariants(k int) {
	w.t.Helper()
	block := w.has(paths.RestoreBlock(w.team))
	if w.db() == "" && !block {
		w.t.Errorf("kill %d: db is absent and restore.block too", k)
	}
	if s := w.step(); w.db() == "restored" && !block && s != StepStart && s != StepClean && s != "" {
		w.t.Errorf("kill %d: the restored db may start with the record at %s", k, s)
	}
}

// finish resumes, or runs afresh once a resume has undone the restore, until it is done.
func (w *world) finish(k int, req Request) {
	w.t.Helper()
	for range 4 {
		if w.step() == "" {
			if w.db() == "restored" {
				return
			}
			if _, err := run(w, req); err != nil {
				w.t.Fatalf("kill %d: the fresh run: %v", k, err)
			}
			continue
		}
		if _, err := resume(w); err != nil && !strings.Contains(err.Error(), "--resume") && !strings.Contains(err.Error(), "not done") {
			w.t.Fatalf("kill %d: the resume at %s: %v", k, w.step(), err)
		}
	}
	w.t.Fatalf("kill %d: not restored after four runs", k)
}

// SM5: a kill at every effect of the online and the offline restore leaves the invariants, and
// the resume (or a fresh run, after a resume from staging) restores the team.
func TestRunKillAtEveryEffect(t *testing.T) {
	for name, c := range map[string]struct {
		world func(t *testing.T) *world
		req   func(w *world) Request
		calls func(w *world) []string
	}{
		"online":  {newWorld, (*world).request, happyCalls},
		"offline": {offlineWorld, (*world).offline, offlineCalls},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for k := 1; k <= len(c.calls(newWorld(t))); k++ {
				t.Run(strconv.Itoa(k), func(t *testing.T) {
					t.Parallel()
					w := c.world(t)
					w.kill = k
					killRun(t, w, c.req(w))
					w.invariants(k)
					w.kill = 0
					w.finish(k, c.req(w))
					w.restored()
					if w.floor().Epoch != 3 {
						t.Errorf("kill %d: floor = %+v", k, w.floor())
					}
				})
			}
		})
	}
}

// RS12: a kill between RS6 step 6's record write and the block's removal leaves both, and the
// resume removes the block and serves.
func TestResumeRemovesTheBlockAtStart(t *testing.T) {
	w := newWorld(t)
	w.kill = at(w, "remove restore.block")
	killRun(t, w, w.request())
	if w.step() != StepStart || !w.has(paths.RestoreBlock(w.team)) || w.db() != "restored" {
		t.Fatalf("step %s, block %v, db %q", w.step(), w.has(paths.RestoreBlock(w.team)), w.db())
	}
	w.kill = 0
	if _, err := resume(w); err != nil {
		t.Fatal(err)
	}
	w.restored()
}

// RS7: a kill after the bootstrap wrote the next rev resumes, since the rev is at least the
// merged one.
func TestResumeAfterTheBootstrapWrote(t *testing.T) {
	w := newWorld(t)
	w.bootRev = 1
	w.kill = at(w, "write restore.json=clean")
	killRun(t, w, w.request())
	w.kill = 0
	if _, err := resume(w); err != nil {
		t.Fatal(err)
	}
	w.restored()
}

// RS8 step 2: a --roster-from-backup restore killed at swap, start or clean resumes, raises
// min_backup past every backup, and a fresh restore of backup 7 is then refused.
func TestResumeFromBackup(t *testing.T) {
	for _, effect := range []string{"write restore.block", "remove restore.block", "removetree db.previous"} {
		t.Run(effect, func(t *testing.T) {
			w := newWorld(t)
			req := Request{Team: w.team, Backup: backupN, FromBackup: true}
			writeFile(t, w.state+"/backups/meta/5", "1\n")
			w.kill = slices.Index(fromBackupCalls(w), effect) + 1
			killRun(t, w, req)
			w.kill = 0
			if _, err := resume(w); err != nil {
				t.Fatal(err)
			}
			w.restored()
			if f := w.floor(); f.MinBackup != backupN+1 || f.Epoch <= 2 {
				t.Errorf("floor = %+v", f)
			}
			_, err := run(w, w.request())
			refusedWith(t, err, "older than backup 8")
		})
	}
}

// RS8 step 2 never lowers min_backup.
func TestCleanKeepsAHigherMinBackup(t *testing.T) {
	w := newWorld(t)
	req := Request{Team: w.team, Backup: backupN, FromBackup: true}
	w.kill = slices.Index(fromBackupCalls(w), "write restore.json=clean") + 1
	killRun(t, w, req)
	w.kill = 0
	epoch := w.floor().Epoch
	w.put(paths.RestoreEpochFile(w.team), string(EncodeEpochFloor(EpochFloor{Epoch: epoch, MinBackup: 40})))
	mark := len(w.calls)
	if _, err := resume(w); err != nil {
		t.Fatal(err)
	}
	if w.floor().MinBackup != 40 || slices.Contains(w.callsSince(mark), "write restore-epoch.json") {
		t.Errorf("floor = %+v, calls %q", w.floor(), w.callsSince(mark))
	}
}

// RS6 step 1 and RE1: the raised epoch is on disk, 0600, with no temporary beside it, before
// the record leaves staged; a resume does not raise it again.
func TestEpochRaiseIsAtomicAndDurable(t *testing.T) {
	w := newWorld(t)
	w.kill = at(w, "write restore.json=swap")
	killRun(t, w, w.request())
	if w.floor().Epoch != 3 || w.staged().Step != StepStaged {
		t.Fatalf("floor = %+v", w.floor())
	}
	info, err := os.Lstat(w.os.Root + paths.RestoreEpochFile(w.team))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("restore-epoch.json = %v, %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Dir(w.os.Root + paths.RestoreEpochFile(w.team)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temporary %s is left", e.Name())
		}
	}
	w.kill = 0
	mark := len(w.calls)
	if _, err = resume(w); err != nil {
		t.Fatal(err)
	}
	if since := w.callsSince(mark); slices.Contains(since, "write restore-epoch.json") || slices.Contains(since, "backup") {
		t.Errorf("the resume ran %q", since)
	}
	w.restored()
}

// A failed epoch write leaves the restore staged.
func TestEpochWriteFailureStaysStaged(t *testing.T) {
	w := newWorld(t)
	w.failAt("write restore-epoch.json")
	_, err := run(w, w.request())
	errWith(t, err, "restore not done, staged")
	w.staged()
}

// RS6 step 3: a staging unit still active is refused before any rename, and the revert puts
// the team back.
func TestSwapRefusesAnActiveStagingUnit(t *testing.T) {
	w := newWorld(t)
	w.hooks["write restore.block"] = func() {
		w.units[paths.RestoreUnit(w.team)] = &fakeUnit{st: UnitState{Load: "loaded", Active: "active"}}
	}
	_, err := run(w, w.request())
	errWith(t, err, "restore not done, reverted", "is active, so nothing is renamed")
	if slices.ContainsFunc(w.calls, func(c string) bool { return strings.HasPrefix(c, "rename") }) {
		t.Errorf("calls = %q", w.calls)
	}
	if r := w.staged(); r.Served || w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		t.Errorf("served %v, or the team does not serve", r.Served)
	}
}

// RS11 from start: a failed bootstrap reverts (RS18 accepting the stopped unit failed), the
// record is staged and served, and the discard names the served events.
func TestBootstrapFailureRevertsAndDiscardNamesTheServedEvents(t *testing.T) {
	w := newWorld(t)
	w.failAt("bootstrap")
	w.stopFails = true
	_, err := run(w, w.request())
	errWith(t, err, "restore not done, reverted", "injected bootstrap", "--resume or --discard")
	r := w.staged()
	if !r.Served || r.Pre != preM || w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		t.Errorf("record = %+v", r)
	}
	if exists(t, w.state+"/"+namePrevious) {
		t.Error("db.previous is left")
	}
	a, err := discard(w)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.Notes, "the events posted while the restored copy served are discarded with it") {
		t.Errorf("notes = %q", a.Notes)
	}
	if got := string(a.Encode()); !strings.Contains(got, `"discarded":true`) {
		t.Errorf("answer = %s", got)
	}
	w.unstaged()
}

// RO6: an offline revert does not start the old database; the team is down at staged.
func TestOfflineRevertLeavesTheTeamDown(t *testing.T) {
	w := offlineWorld(t)
	w.failAt("bootstrap")
	_, err := run(w, w.offline())
	errWith(t, err, "team ops is down: the restored database did not start", "injected bootstrap", "--resume or --discard")
	w.staged()
	if since := w.callsSince(w.index("child revert")); slices.Contains(since, "serve") {
		t.Errorf("the old database was started: %q", since)
	}
}

// RS11 steps 2 to 7: a failure leaves the record at revert; the resume repeats from step 2.
func TestRevertFailureIsResumed(t *testing.T) {
	for _, effect := range []string{"child revert", "serve"} {
		t.Run(effect, func(t *testing.T) {
			w := newWorld(t)
			w.failAt("bootstrap")
			w.hooks["bootstrap"] = func() { w.failAt(effect) }
			_, err := run(w, w.request())
			errWith(t, err, "team ops is down: restore --resume or --discard after fixing", "injected "+effect, "injected bootstrap")
			if w.step() != StepRevert {
				t.Fatalf("step = %s", w.step())
			}
			w.fail = map[string]error{}
			_, err = resume(w)
			errWith(t, err, "restore not done, reverted", "interrupted")
			w.staged()
		})
	}
}

// RS16 during a revert: an entry the engine did not make leaves the record at revert with the
// listing, and the block; the discard then removes only the record.
func TestStuckRevertAndItsDiscard(t *testing.T) {
	w := newWorld(t)
	w.failAt("bootstrap")
	w.hooks["bootstrap"] = func() { writeFile(t, w.state+"/"+restoreName(w.team, backupN), "plant") }
	_, err := run(w, w.request())
	errWith(t, err, "team ops is down", "db.restore-7=file", "restore --team ops --discard")
	r := w.record()
	if r.Step != StepRevert || !strings.Contains(r.Stuck, "db.restore-7=file") || !w.has(paths.RestoreBlock(w.team)) {
		t.Fatalf("record = %+v", r)
	}
	if _, err = resume(w); err == nil || w.step() != StepRevert {
		t.Errorf("the resume = %v", err)
	}
	a, err := discard(w)
	if err != nil {
		t.Fatal(err)
	}
	if w.has(paths.RestoreRecord(w.team)) || !w.has(paths.RestoreBlock(w.team)) || len(a.Notes) != 1 ||
		!strings.Contains(a.Notes[0], "restore.block stays") {
		t.Errorf("notes = %q", a.Notes)
	}
}

// RS8: a failed clean-up answers Printed and the resume completes; a failed status refresh
// answers Printed with nothing left.
func TestCleanUpFailures(t *testing.T) {
	w := newWorld(t)
	w.failAt("child finish")
	a, err := run(w, w.request())
	errWith(t, err, "restored; clean-up failed", "injected child finish", "restore --team ops --resume")
	if !a.Printed || w.step() != StepClean || a.Pre != preM {
		t.Errorf("answer = %+v, step %s", a, w.step())
	}
	w.fail = map[string]error{}
	if a, err = resume(w); err != nil || a.Printed {
		t.Fatalf("the resume = %+v, %v", a, err)
	}
	w.restored()
	w = newWorld(t)
	w.failAt("status")
	a, err = run(w, w.request())
	errWith(t, err, "restored; status refresh failed")
	if !a.Printed {
		t.Error("not Printed")
	}
	w.restored()
	w = newWorld(t)
	w.failAt("write restore.json=clean")
	a, err = run(w, w.request())
	errWith(t, err, "restored; clean-up failed")
	if !a.Printed || w.step() != StepStart {
		t.Errorf("answer %+v at %s", a, w.step())
	}
}

// RS13: the discard is refused at swap, start and clean, naming --resume.
func TestDiscardRefusedPastStaged(t *testing.T) {
	for _, effect := range []string{"write restore.block", "remove restore.block", "removetree db.previous"} {
		t.Run(effect, func(t *testing.T) {
			w := newWorld(t)
			w.kill = at(w, effect)
			killRun(t, w, w.request())
			_, err := discard(w)
			refusedWith(t, err, "--resume")
			if !w.has(paths.RestoreRecord(w.team)) {
				t.Error("the record was removed")
			}
		})
	}
}

// RS13: a discard at staged whose pre-restore backup pruned meta/7 names the only copy; at
// staging it stops the staging unit and removes the copy.
func TestDiscardAtStagedAndStaging(t *testing.T) {
	w := newWorld(t)
	w.kill = at(w, "child export")
	killRun(t, w, w.request())
	removeFile(w, paths.HomeserverPrivateBackupMeta(w.team)+"/7")
	a, err := discard(w)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.Notes, "the discarded copy was the only copy of backup 7") {
		t.Errorf("notes = %q", a.Notes)
	}
	w.unstaged()
	w = newWorld(t)
	w.kill = at(w, "child secure")
	killRun(t, w, w.request())
	if a, err = discard(w); err != nil || len(a.Notes) != 0 {
		t.Fatalf("discard = %+v, %v", a, err)
	}
	if !w.unit(paths.RestoreUnit(w.team)).st.Stopped() {
		t.Error("the staging unit was not stopped")
	}
	w.unstaged()
}

// Resume and discard refuse when no restore is recorded, and fail on a bad record.
func TestResumeAndDiscardNeedARecord(t *testing.T) {
	w := newWorld(t)
	_, err := resume(w)
	refusedWith(t, err, "nothing to resume or discard")
	_, err = discard(w)
	refusedWith(t, err, "nothing to resume or discard")
	w.put(paths.RestoreRecord(w.team), "{")
	if _, err = resume(w); err == nil {
		t.Error("a bad record was resumed")
	}
	if _, err = discard(w); err == nil {
		t.Error("a bad record was discarded")
	}
}

// A resume reads the tar and the applied files again.
func TestResumeNeedsTheTarAndTheAppliedTeam(t *testing.T) {
	w := newWorld(t)
	w.kill = at(w, "child export")
	killRun(t, w, w.request())
	w.kill = 0
	w.tarErr = errInterrupted
	if _, err := resume(w); err == nil {
		t.Error("resumed without the tar")
	}
	w.tarErr = nil
	removeFile(w, paths.ServerConfig(w.team))
	if _, err := resume(w); err == nil {
		t.Error("resumed without the config")
	}
}

// Every effect from RS6 on, failing once, leaves the invariants, and resumes (or a fresh run)
// restore the team; a failure of the live check is the homeserver lost (69).
func TestEveryLaterFailureRecovers(t *testing.T) {
	w := newWorld(t)
	calls := happyCalls(w)
	for _, effect := range slices.Compact(slices.Sorted(slices.Values(calls[slices.Index(calls, "write restore-epoch.json"):]))) {
		t.Run(effect, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.failAt(effect)
			_, err := run(w, w.request())
			if err == nil {
				t.Fatal("no error")
			}
			if effect == "child check staged=false" && !errors.Is(err, matrix.ErrUnreachable) {
				t.Errorf("a failed live check = %v, want the homeserver lost", err)
			}
			w.invariants(0)
			w.fail = map[string]error{}
			w.finish(0, w.request())
			w.restored()
		})
	}
}

// A failure while the revert records itself leaves the team down, and is resumed.
func TestRevertRecordFailure(t *testing.T) {
	w := newWorld(t)
	w.failAt("bootstrap")
	w.failAt("write restore.json=revert")
	_, err := run(w, w.request())
	errWith(t, err, "team ops is down", "injected write restore.json=revert")
	w.fail = map[string]error{}
	w.finish(0, w.request())
	w.restored()
}
