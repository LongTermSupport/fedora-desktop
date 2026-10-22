package restore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// RS3's wait for a stopped staging unit to unload: ten polls a second apart.
const (
	unloadPolls = 10
	unloadEvery = time.Second
)

// errInterrupted is the cause a resume gives for a run that died mid-step.
var errInterrupted = errors.New("an earlier run of it was interrupted")

// Answer is what the verb prints (RS9), or a discard's (RS13). Printed says the restore is in
// place although the error beside it failed a later step (RS8), so the verb prints both.
type Answer struct {
	Team      paths.TeamID
	Backup    int64
	Offline   bool
	Pre       int64
	Lost      *Lost
	SinceMS   int64
	Lists     Lists
	Notes     []string
	Changed   []string
	Discarded bool
	Printed   bool
}

// Encode is the answer's JSON.
func (a Answer) Encode() []byte {
	members := []proto.Member{proto.M("v", proto.Int(1)), proto.M("team", proto.Str(a.Team.String())), proto.M("backup", proto.Int(a.Backup))}
	if a.Discarded {
		return proto.Obj(append(members, proto.M("discarded", proto.Bool(true)), proto.M("notes", strArray(a.Notes)))...).Compact()
	}
	lost := proto.Obj(proto.M("exported", proto.Bool(false)), proto.M("since_ms", proto.Int(a.SinceMS)))
	if l := a.Lost; l != nil {
		lost = proto.Obj(proto.M("count", proto.Int(l.Count)), proto.M("first_ms", proto.Int(l.FirstMS)), proto.M("end_ms", proto.Int(l.EndMS)),
			proto.M("truncated", proto.Bool(l.Truncated)), proto.M("path", proto.Str(paths.RestoreLostFile(a.Team, a.Backup))))
	}
	l := a.Lists
	return proto.Obj(append(members,
		proto.M("offline", proto.Bool(a.Offline)), proto.M("pre", nullableInt(a.Pre)), proto.M("lost", lost),
		proto.M("reissue", strArray(l.Reissue)), proto.M("return", strArray(l.Return)), proto.M("password", strArray(l.Password)),
		proto.M("unknown", strArray(l.Unknown)), proto.M("pending", nullable(l.Pending)), proto.M("notes", strArray(a.Notes)),
		proto.M("changed", strArray(a.Changed)),
	)...).Compact()
}

// engine is one run of the restore of rec's team: what the record and the applied files hold,
// and the CHANGED lines of this run.
type engine struct {
	d        Deps
	held     *teamlock.Lock
	rec      Record
	tar      Tar
	teamFile []byte
	config   []byte
	decl     declare.Team
	changed  []string
}

// Run is a fresh restore of req.Backup under held (steps 01-10): validated, recorded, staged,
// swapped in, started and cleaned up.
func Run(ctx context.Context, d Deps, held *teamlock.Lock, req Request) (Answer, error) {
	v, err := Validate(ctx, d, req)
	if err != nil {
		return Answer{}, err
	}
	e := &engine{
		d: d, held: held, tar: v.Tar, teamFile: v.TeamFile, config: v.Config, decl: v.Team,
		rec: Record{
			Team: req.Team, Backup: req.Backup, Offline: req.Offline, FromBackup: req.FromBackup,
			Roster: v.Merged.Roster, Lists: v.Merged.Lists,
		},
	}
	if err = e.write(StepStaging); err != nil {
		return Answer{}, fmt.Errorf("restore not done: %w", err)
	}
	if err = e.stage(ctx); err == nil {
		err = e.write(StepStaged)
	}
	if err != nil {
		return Answer{}, e.unstage(ctx, err)
	}
	return e.fromStaged(ctx)
}

// Resume goes on with the recorded restore of team from its step (RS12).
func Resume(ctx context.Context, d Deps, held *teamlock.Lock, team paths.TeamID) (Answer, error) {
	rec, err := readRecord(d, team)
	if err != nil {
		return Answer{}, err
	}
	tar, err := readTar(d, team, rec.Backup)
	if err != nil {
		return Answer{}, err
	}
	v, err := readApplied(d, team)
	if err != nil {
		return Answer{}, err
	}
	e := &engine{d: d, held: held, rec: rec, tar: tar, teamFile: v.TeamFile, config: v.Config, decl: v.Team}
	switch rec.Step {
	case StepStaging:
		return Answer{}, e.unstage(ctx, errInterrupted)
	case StepStaged:
		return e.fromStaged(ctx)
	case StepSwap:
		return e.swapIn(ctx)
	case StepStart:
		if err = d.FS.Remove(paths.RestoreBlock(team)); err != nil {
			return e.revert(ctx, true, err)
		}
		return e.start(ctx)
	case StepClean:
		return e.cleanUp(ctx)
	case StepRevert:
	}
	return e.revertFromBlock(ctx, errInterrupted)
}

// Discard drops a recorded restore that has not swapped anything in (RS13), or the record of a
// revert RS16 refused, leaving its block.
func Discard(ctx context.Context, d Deps, team paths.TeamID) (Answer, error) {
	rec, err := readRecord(d, team)
	if err != nil {
		return Answer{}, err
	}
	e := &engine{d: d, rec: rec}
	a := Answer{Team: team, Backup: rec.Backup, Discarded: true, Notes: []string{}}
	switch {
	case rec.Step == StepRevert && rec.Stuck != "":
		a.Notes = append(a.Notes, fmt.Sprintf("%s stays, so nothing starts the homeserver: inspect %s, which holds %s",
			paths.RestoreBlock(team), paths.HomeserverPrivateState(team), rec.Stuck))
		return a, d.FS.Remove(paths.RestoreRecord(team))
	case rec.Step != StepStaging && rec.Step != StepStaged:
		return Answer{}, refusedf("the restore of team %s is at %s, which a discard cannot undo: run restore --team %s --resume",
			team, rec.Step, team)
	}
	if err = e.unstaged(ctx, false); err != nil {
		return Answer{}, err
	}
	if rec.Pre != 0 {
		k, kindErr := d.FS.Kind(team, metaName(team, rec.Backup))
		if kindErr != nil {
			return Answer{}, kindErr
		}
		if k != KindFile {
			a.Notes = append(a.Notes, fmt.Sprintf("the discarded copy was the only copy of backup %d", rec.Backup))
		}
	}
	if rec.Served {
		a.Notes = append(a.Notes, "the events posted while the restored copy served are discarded with it")
	}
	return a, nil
}

// readRecord is the team's record; none is a refusal.
func readRecord(d Deps, team paths.TeamID) (Record, error) {
	raw, found, err := state.ReadOwnedFile(d.Root+paths.RestoreRecord(team), maxRecord, d.StateOwnerUID)
	if err != nil {
		return Record{}, err
	}
	if !found {
		return Record{}, refusedf("no restore of team %s is recorded: there is nothing to resume or discard", team)
	}
	return DecodeRecord(raw, team)
}

func (e *engine) team() paths.TeamID { return e.rec.Team }

// write records the step; every write is fsynced (state.WriteFile).
func (e *engine) write(step Step) error {
	e.rec.Step = step
	return e.d.FS.WriteFile(paths.RestoreRecord(e.team()), EncodeRecord(e.rec))
}

// plantedError is RS2's refusal of a db.restore-<N> that appeared after RV2: it is not removed,
// so the next validation names it.
type plantedError struct{ error }

func (e *plantedError) Unwrap() error { return e.error }

// stage is RS2 and RS3: the staging config, the staged and secured copy, and its verify.
func (e *engine) stage(ctx context.Context) error {
	t, n := e.team(), e.rec.Backup
	cfg, err := restoreConfig(e.config, t, n)
	if err != nil {
		return err
	}
	version, err := e.d.Version(t)
	if err != nil {
		return err
	}
	s := Staging{Team: t, Backup: n, Version: version, Port: e.decl.Port, Sandbox: e.d.Sandbox(t)}
	restoring, err := StagingArgv(s, true)
	if err != nil {
		return err
	}
	reopen, err := StagingArgv(s, false)
	if err != nil {
		return err
	}
	secure, err := SecureArgv(t, e.d.Self)
	if err != nil {
		return err
	}
	if err = e.d.FS.WriteFile(paths.RestoreConfig(t), cfg); err != nil {
		return err
	}
	k, err := e.d.FS.Kind(t, restoreName(t, n))
	if err != nil {
		return err
	}
	if k != KindAbsent {
		return &plantedError{refusedf("%s/%s appeared after validation (a %s): inspect and remove it first",
			paths.HomeserverPrivateState(t), restoreName(t, n), k)}
	}
	if err = e.secure(ctx, restoring, secure); err != nil {
		return err
	}
	if err = e.verify(ctx, reopen, secure); err != nil {
		return err
	}
	return e.d.FS.Syncfs(t)
}

// secure is RS3's first half: the restore and RC1 in one staging run, then its stop and unload.
func (e *engine) secure(ctx context.Context, restoring, secure []string) error {
	t := e.team()
	inv, err := e.startStaging(ctx, restoring)
	if err != nil {
		return err
	}
	l := e.rec.Lists
	changed, err := e.d.Child.Secure(ctx, t, secure, SecureArgs{
		TeamFile: e.teamFile, Port: e.decl.Port, Token: e.tar.Token, Rooms: e.tar.Rooms, Roster: e.rec.Roster,
		Retire: l.Retire, Revoke: l.Revoke, RevokeHumans: l.RevokeHumans, Lock: l.Lock,
	})
	if err != nil {
		return err
	}
	e.changed = append(e.changed, changed...)
	if err = e.sameRun(ctx, inv); err != nil {
		return err
	}
	if err = e.stopUnit(ctx, paths.RestoreUnit(t)); err != nil {
		return err
	}
	return e.waitUnloaded(ctx)
}

// verify is RS3's reopen: the stopped copy opened as it is answers the merged epoch and rev.
func (e *engine) verify(ctx context.Context, reopen, secure []string) error {
	t := e.team()
	inv, err := e.startStaging(ctx, reopen)
	if err != nil {
		return err
	}
	got, err := e.d.Child.Check(ctx, t, secure, CheckArgs{TeamFile: e.teamFile, Port: e.decl.Port, Token: e.tar.Token})
	if err != nil {
		return err
	}
	if want := e.rec.Roster; got.RestoreEpoch != want.RestoreEpoch || got.Rev != want.Rev {
		return fmt.Errorf("the secured copy, reopened, serves restore epoch %d rev %d, not the merged epoch %d rev %d",
			got.RestoreEpoch, got.Rev, want.RestoreEpoch, want.Rev)
	}
	if err = e.sameRun(ctx, inv); err != nil {
		return err
	}
	return e.stopUnit(ctx, paths.RestoreUnit(t))
}

// startStaging runs the staging unit (RU1) and answers its InvocationID.
func (e *engine) startStaging(ctx context.Context, argv []string) (string, error) {
	unit := paths.RestoreUnit(e.team())
	if _, err := e.d.Run.Run(ctx, unitCommand(argv, nil, nil)); err != nil {
		return "", fmt.Errorf("the staging unit %s did not start: %w", unit, err)
	}
	st, err := e.d.Units.State(ctx, unit)
	if err != nil {
		return "", err
	}
	if st.Active != activeActive {
		return "", fmt.Errorf("the staging unit %s is %s after its start", unit, st.Active)
	}
	return st.InvocationID, nil
}

// sameRun is RS3's check that the staging unit never restarted since inv started.
func (e *engine) sameRun(ctx context.Context, inv string) error {
	unit := paths.RestoreUnit(e.team())
	st, err := e.d.Units.State(ctx, unit)
	if err != nil {
		return err
	}
	if st.InvocationID != inv || st.NRestarts != 0 || st.Active != activeActive {
		return fmt.Errorf("the staging unit %s is not the run it was (invocation %s, %d restarts, %s), so its copy is not the one secured",
			unit, st.InvocationID, st.NRestarts, st.Active)
	}
	return nil
}

// waitUnloaded is RS3's poll until --collect unloads the stopped staging unit.
func (e *engine) waitUnloaded(ctx context.Context) error {
	unit := paths.RestoreUnit(e.team())
	for i := 0; ; i++ {
		st, err := e.d.Units.State(ctx, unit)
		if err != nil {
			return err
		}
		if st.NotLoaded() {
			return nil
		}
		if i == unloadPolls {
			return fmt.Errorf("the staging unit %s is still loaded %d s after its stop", unit, unloadPolls)
		}
		if err = e.d.Clock.Sleep(ctx, unloadEvery); err != nil {
			return err
		}
	}
}

// stopUnit stops a loaded unit, which must then be stopped (RS18).
func (e *engine) stopUnit(ctx context.Context, unit string) error {
	st, err := e.d.Units.State(ctx, unit)
	if err != nil || st.NotLoaded() {
		return err
	}
	if err = e.d.Units.Stop(ctx, unit); err != nil {
		return err
	}
	if st, err = e.d.Units.State(ctx, unit); err != nil {
		return err
	}
	if !st.Stopped() {
		return fmt.Errorf("%s is %s after its stop", unit, st.Active)
	}
	return nil
}

// unstage is RS10: the staging units stopped, the copy, the config and the record removed.
func (e *engine) unstage(ctx context.Context, cause error) error {
	planted, ok := errors.AsType[*plantedError](cause)
	if err := e.unstaged(ctx, ok && planted != nil); err != nil {
		return fmt.Errorf("restore not done: %w; its clean-up failed: %w (the record stays at staging: restore --team %s --resume or --discard)",
			cause, err, e.team())
	}
	return fmt.Errorf("restore not done: %w", cause)
}

// unstaged is RS10's and RS13's clean-up; keepCopy leaves db.restore-<N> as it is.
func (e *engine) unstaged(ctx context.Context, keepCopy bool) error {
	t := e.team()
	err := errors.Join(e.stopUnit(ctx, paths.RestoreUnit(t)), e.stopUnit(ctx, paths.RestoreCtlUnit(t)))
	if err == nil && !keepCopy {
		err = e.d.FS.RemoveTree(t, restoreName(t, e.rec.Backup))
	}
	if err == nil {
		err = e.d.FS.Remove(paths.RestoreConfig(t))
	}
	if err == nil {
		err = e.d.FS.Remove(paths.RestoreRecord(t))
	}
	return err
}

// fromStaged is RS4 and RS5, then the swap; either failing leaves the team serving, staged.
func (e *engine) fromStaged(ctx context.Context) (Answer, error) {
	r, t := &e.rec, e.team()
	if !r.Offline && !r.FromBackup && r.Pre == 0 {
		if err := e.preBackup(ctx); err != nil {
			return Answer{}, e.stillStaged(fmt.Errorf("the pre-restore backup: %w", err))
		}
	}
	if !r.Offline {
		x, err := e.d.Child.Export(ctx, t, ExportArgs{TeamFile: e.teamFile, Backup: r.Backup, SinceMS: e.tar.TimeMS})
		if err == nil {
			r.Lost = &Lost{Count: x.Count, FirstMS: x.FirstMS, EndMS: x.EndMS, Truncated: x.Truncated}
			err = e.write(StepStaged)
		}
		if err != nil {
			return Answer{}, e.stillStaged(fmt.Errorf("the export: %w", err))
		}
	}
	return e.swap(ctx)
}

// preBackup is RS4: backup M of the live server, recorded as pre.
func (e *engine) preBackup(ctx context.Context) error {
	m, err := e.d.Backup(ctx, e.held, []int64{e.rec.Backup})
	if errors.Is(err, backup.ErrNotRunning) {
		return &serverLostError{err}
	}
	if err == nil && (m < 1 || m > maxBackup) {
		err = fmt.Errorf("the pre-restore backup answered the number %d", m)
	}
	if err == nil {
		e.rec.Pre = m
		err = e.write(StepStaged)
	}
	return err
}

// serverLostError is RS15's 69 for a homeserver that answered at validation and was not
// running at RS4's backup: it is matrix.ErrUnreachable and hides BT1's not running, which is
// the backup verb's refusal (70) only.
type serverLostError struct{ err error }

func (e *serverLostError) Error() string { return e.err.Error() }

// Is makes a serverLostError matrix.ErrUnreachable.
func (e *serverLostError) Is(target error) bool { return target == matrix.ErrUnreachable }

func (e *engine) stillStaged(err error) error {
	return fmt.Errorf("restore not done, staged: %w; restore --team %s --resume or --discard", err, e.team())
}

// swap is RS6's steps 1 and 2: the high-water epoch raised before the secured copy can serve,
// then the record at swap.
func (e *engine) swap(ctx context.Context) (Answer, error) {
	t := e.team()
	floor, err := readFloor(e.d, t)
	if epoch := e.rec.Roster.RestoreEpoch; err == nil && epoch > floor.Epoch {
		err = e.d.FS.WriteFile(paths.RestoreEpochFile(t), EncodeEpochFloor(floor.Raised(EpochFloor{Epoch: epoch})))
	}
	if err == nil {
		err = e.write(StepSwap)
	}
	if err != nil {
		return Answer{}, e.stillStaged(err)
	}
	return e.swapIn(ctx)
}

// swapIn is RS6 from its step 2; a failure is reverted.
func (e *engine) swapIn(ctx context.Context) (Answer, error) {
	if err := e.swapped(ctx); err != nil {
		return e.revert(ctx, false, err)
	}
	return e.start(ctx)
}

// swapped is RS6's steps 2 to 6: the block, every unit stopped, the renames, RC2, then the
// record at start and the block removed.
func (e *engine) swapped(ctx context.Context) error {
	t := e.team()
	if err := e.d.FS.WriteFile(paths.RestoreBlock(t), nil); err != nil {
		return err
	}
	if err := e.stopUnit(ctx, paths.ServerUnit(t)); err != nil {
		return err
	}
	for _, unit := range []string{paths.ServerUnit(t), paths.RestoreUnit(t), paths.RestoreCtlUnit(t)} {
		st, err := e.d.Units.State(ctx, unit)
		if err != nil {
			return err
		}
		if !st.Stopped() {
			return fmt.Errorf("%s is %s, so nothing is renamed", unit, st.Active)
		}
	}
	if err := Renames(e.d.FS, t, e.rec.Backup, Swapped); err != nil {
		return err
	}
	if err := e.d.FS.Fsync(t); err != nil {
		return err
	}
	changed, err := e.d.Child.Swap(ctx, t, SwapArgs{TeamFile: e.teamFile, Token: e.tar.Token, Rooms: e.tar.Rooms, Roster: e.rec.Roster})
	if err != nil {
		return err
	}
	e.changed = append(e.changed, changed...)
	if err = e.write(StepStart); err != nil {
		return err
	}
	return e.d.FS.Remove(paths.RestoreBlock(t))
}

// start is RS7; a failure is reverted, and the restored copy may have served.
func (e *engine) start(ctx context.Context) (Answer, error) {
	if err := e.started(ctx); err != nil {
		return e.revert(ctx, true, err)
	}
	if err := e.write(StepClean); err != nil {
		return e.cleanFailed(err)
	}
	return e.cleanUp(ctx)
}

// started is RS7: the start behind RS17, RC8's read of the served epoch, then the bootstrap.
func (e *engine) started(ctx context.Context) error {
	t := e.team()
	if err := Startable(e.d.FS, t); err != nil {
		return err
	}
	if err := e.d.Serve(ctx, t); err != nil {
		return err
	}
	got, err := e.d.Child.Check(ctx, t, nil, CheckArgs{TeamFile: e.teamFile})
	if err != nil {
		return err
	}
	if want := e.rec.Roster; got.RestoreEpoch != want.RestoreEpoch || got.Rev < want.Rev {
		return fmt.Errorf("the restored homeserver serves restore epoch %d rev %d, not the merged epoch %d at rev %d or later",
			got.RestoreEpoch, got.Rev, want.RestoreEpoch, want.Rev)
	}
	key, err := e.d.RegKey(e.config)
	if err != nil {
		return err
	}
	changed, err := e.d.Bootstrap(ctx, host.Bootstrap{Input: declare.Input{Team: e.decl}, TeamFile: e.teamFile, RegKey: key})
	e.changed = append(e.changed, changed...)
	return err
}

// cleanUp is RS8 from its step 2; the restore serves whatever fails.
func (e *engine) cleanUp(ctx context.Context) (Answer, error) {
	if err := e.cleaned(ctx); err != nil {
		return e.cleanFailed(err)
	}
	if err := e.d.Status.Refresh(ctx, e.team(), false, []string{e.event()}); err != nil {
		a := e.answer()
		a.Printed = true
		return a, fmt.Errorf("restored; status refresh failed: %w", err)
	}
	return e.answer(), nil
}

func (e *engine) cleanFailed(err error) (Answer, error) {
	a := e.answer()
	a.Printed = true
	return a, fmt.Errorf("restored; clean-up failed: %w; restore --team %s --resume", err, e.team())
}

// cleaned is RS8's steps 2 to 6.
func (e *engine) cleaned(ctx context.Context) error {
	t := e.team()
	if e.rec.FromBackup {
		if err := e.raiseMinBackup(); err != nil {
			return err
		}
	}
	if err := e.d.FS.RemoveTree(t, namePrevious); err != nil {
		return err
	}
	if err := e.d.FS.Remove(paths.RestoreConfig(t)); err != nil {
		return err
	}
	changed, err := e.d.Child.Finish(ctx, t, e.teamFile)
	if err != nil {
		return err
	}
	e.changed = append(e.changed, changed...)
	return e.d.FS.Remove(paths.RestoreRecord(t))
}

// raiseMinBackup is RS8's step 2 (RM11): no backup made so far may be restored again, since its
// seqs cannot be ordered against the roster a --roster-from-backup restore drew.
func (e *engine) raiseMinBackup() error {
	t := e.team()
	floor, err := readFloor(e.d, t)
	if err != nil {
		return err
	}
	entries, err := e.d.FS.ReadDir(t, metaDir(t))
	if err != nil {
		return err
	}
	highest := e.rec.Backup
	for _, entry := range entries {
		n, convErr := strconv.ParseInt(entry.Name, 10, 64)
		if convErr == nil && entry.Kind == KindFile && strconv.FormatInt(n, 10) == entry.Name {
			highest = max(highest, n)
		}
	}
	if highest+1 <= floor.MinBackup {
		return nil
	}
	return e.d.FS.WriteFile(paths.RestoreEpochFile(t), EncodeEpochFloor(floor.Raised(EpochFloor{MinBackup: highest + 1})))
}

// revert is RS11 from its step 1; from start, the restored copy may have served.
func (e *engine) revert(ctx context.Context, fromStart bool, cause error) (Answer, error) {
	e.rec.Served = e.rec.Served || fromStart
	if err := e.write(StepRevert); err != nil {
		return Answer{}, e.down(err, cause)
	}
	return e.revertFromBlock(ctx, cause)
}

// revertFromBlock is RS11 from its step 2, ending at staged.
func (e *engine) revertFromBlock(ctx context.Context, cause error) (Answer, error) {
	t := e.team()
	if err := e.unswapped(ctx); err != nil {
		return Answer{}, e.down(err, cause)
	}
	e.rec.Stuck = ""
	if err := e.write(StepStaged); err != nil {
		return Answer{}, e.down(err, cause)
	}
	if e.rec.Offline {
		return Answer{}, fmt.Errorf("team %s is down: the restored database did not start: %w; restore --resume or --discard", t, cause)
	}
	return Answer{}, fmt.Errorf("restore not done, reverted: %w; restore --team %s --resume or --discard", cause, t)
}

// unswapped is RS11's steps 2 to 7. An RS16 refusal is recorded beside revert.
func (e *engine) unswapped(ctx context.Context) error {
	t := e.team()
	if err := e.d.FS.WriteFile(paths.RestoreBlock(t), nil); err != nil {
		return err
	}
	if err := e.stopUnit(ctx, paths.ServerUnit(t)); err != nil {
		return err
	}
	if err := Renames(e.d.FS, t, e.rec.Backup, Unswapped); err != nil {
		if stuck, ok := errors.AsType[*StuckError](err); ok {
			e.rec.Stuck = stuck.Listing
			return errors.Join(err, e.write(StepRevert))
		}
		return err
	}
	if err := e.d.FS.Fsync(t); err != nil {
		return err
	}
	changed, err := e.d.Child.Revert(ctx, t, e.teamFile)
	if err != nil {
		return err
	}
	e.changed = append(e.changed, changed...)
	if err = e.d.FS.Remove(paths.RestoreBlock(t)); err != nil || e.rec.Offline {
		return err
	}
	if err = Startable(e.d.FS, t); err != nil {
		return err
	}
	return e.d.Serve(ctx, t)
}

// down is RS11's report of a revert that did not finish.
func (e *engine) down(err, cause error) error {
	t := e.team()
	if stuck, ok := errors.AsType[*StuckError](err); ok && stuck != nil {
		return fmt.Errorf("team %s is down: %w; the record and %s stay, so nothing starts it: inspect the directory, then "+
			"restore --team %s --discard (the restore had failed: %w)", t, err, paths.RestoreBlock(t), t, cause)
	}
	return fmt.Errorf("team %s is down: restore --resume or --discard after fixing %w (the restore had failed: %w)", t, err, cause)
}

// answer is RS9's for the record.
func (e *engine) answer() Answer {
	r := e.rec
	notes := slices.Clone(r.Lists.Notes)
	switch {
	case r.Offline:
		notes = append(notes, e.offlineLine())
	case r.Lost != nil:
		notes = append(notes, fmt.Sprintf("events posted between the export's end (%d) and the stop are lost and were not exported", r.Lost.EndMS))
	}
	return Answer{
		Team: r.Team, Backup: r.Backup, Offline: r.Offline, Pre: r.Pre, Lost: r.Lost, SinceMS: e.tar.TimeMS,
		Lists: r.Lists, Notes: notes, Changed: slices.Clone(e.changed),
	}
}

// offlineLine is RO5's.
func (e *engine) offlineLine() string {
	return "offline restore: every event after " + stamp(e.tar.TimeMS) + " is lost and was not exported; no pre-restore " +
		"backup was taken; the database that would not start was removed"
}

// event is the status record's event of RS8's step 7.
func (e *engine) event() string {
	r := e.rec
	if r.Offline || r.Lost == nil {
		return e.offlineLine()
	}
	return fmt.Sprintf("restored backup %d, %d events lost, export at %s", r.Backup, r.Lost.Count, paths.RestoreLostFile(r.Team, r.Backup))
}

// restoreConfig is RS2's config.restore.toml: the live config with its one database_path line
// set to db.restore-<n> and its one address line to the loopback, and its one
// database_backup_path line unchanged.
func restoreConfig(config []byte, team paths.TeamID, n int64) ([]byte, error) {
	set := map[string]string{
		"database_path = ":        `database_path = "` + paths.HomeserverRestoreDatabase(team, n) + `"`,
		"address = ":              `address = ["127.0.0.1", "::1"]`,
		"database_backup_path = ": "",
	}
	seen := map[string]int{}
	lines := strings.Split(string(config), "\n")
	for i, line := range lines {
		for prefix, to := range set {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			seen[prefix]++
			if to != "" {
				if line == to {
					return nil, fmt.Errorf("the config of team %s already names the restore's %s", team, strings.TrimSuffix(prefix, " = "))
				}
				lines[i] = to
			}
		}
	}
	for prefix := range set {
		if seen[prefix] != 1 {
			return nil, fmt.Errorf("the config of team %s has %d %q lines, not one", team, seen[prefix], strings.TrimSuffix(prefix, " = "))
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}
