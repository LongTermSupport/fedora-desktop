package carry

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// Drill is `team export --team T --drill` (SD1-SD5) under the team lock, waited for
// host.PlayWait (SL1), with a guard that refuses a restore, a seal and an import but admits its
// own record, so a drill a kill left is resumed. A fresh drill validates as SC1 does without the
// seal clause (SD1) and records drill.json, with SC2's tail, before any stop (SD2). Then the
// units are stopped without a disable or a block, SC5's facts read and the archive of kind drill
// written to CarryDrillExport (SD3); the team is served again and the record removed. With
// drill.json present, SD1's serving clause is skipped (SD4). A copy that fails still serves the
// team again; a Serve that fails leaves the record and the team down, and says so. Nothing is
// cleaned up in a defer but the lock's release (SM5).
func Drill(ctx context.Context, d Deps, team paths.TeamID) ([]byte, error) {
	maxArchive, err := archiveCap(d)
	if err != nil {
		return nil, err
	}
	if d.Serve == nil {
		return nil, errors.New("this wiring cannot serve a team again after its drill")
	}
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.export", host.PlayWait, d.Clock, drillGuard(d.Root))
	if err != nil {
		return nil, err
	}
	r := &drilling{s: &sealing{d: d, team: team, drill: true}, x: newExporting(d, team, maxArchive, KindDrill)}
	var out []byte
	func() {
		defer func() { err = errors.Join(err, lock.Release()) }()
		out, err = r.run(ctx)
	}()
	return out, err
}

// drillGuard is the drill's guard (SG3, SD5): a restore, a seal at either step and an import
// refuse it; its own record does not.
func drillGuard(root string) teamlock.Guard {
	return teamlock.All(restore.Guard(root), refuseRecords(root, RecordSeal, RecordImport))
}

type drilling struct {
	s *sealing
	x *exporting
}

func (r *drilling) run(ctx context.Context) ([]byte, error) {
	rec, found, err := r.read()
	switch {
	case err != nil:
		return nil, err
	case found:
		if _, _, err = r.s.settled(ctx); err != nil {
			return nil, err
		}
	default:
		tail, verr := r.s.validate(ctx)
		if verr != nil {
			return nil, verr
		}
		rec = DrillRecord{Team: r.s.team, Step: StepStopping, StartedMS: r.s.d.Clock.Now().UnixMilli(), Tail: tail}
		if err = r.s.d.FS.WriteFile(paths.DrillRecord(r.s.team), EncodeDrillRecord(rec)); err != nil {
			return nil, fmt.Errorf("cannot write %s: %w", paths.DrillRecord(r.s.team), err)
		}
	}
	written, facts, copyErr := r.copy(ctx, rec.Tail)
	if err = r.serve(ctx, copyErr); err != nil {
		return nil, err
	}
	if copyErr != nil {
		return nil, copyErr
	}
	return r.x.answer(written, facts.TimeMS), nil
}

// copy is SD3's cold copy: the stop, SC5's facts and the archive.
func (r *drilling) copy(ctx context.Context, tail []TailEntry) (Written, SealFacts, error) {
	if err := r.s.stop(ctx); err != nil {
		return Written{}, SealFacts{}, err
	}
	facts, err := r.s.facts(tail)
	if err != nil {
		return Written{}, SealFacts{}, err
	}
	written, err := r.x.archive(facts)
	return written, facts, err
}

// serve serves the team again and removes the record. A Serve that fails leaves the record and
// says the team is down, with the copy's failure, if any, as text: the exit is the failure's
// (1), never the copy's class.
func (r *drilling) serve(ctx context.Context, copyErr error) error {
	team := r.s.team
	if err := r.s.d.Serve(ctx, team); err != nil {
		down := fmt.Errorf("the team is down: %s again after fixing %w", r.s.again(), err)
		if copyErr != nil {
			down = fmt.Errorf("%w (the copy had failed too: %s)", down, copyErr.Error())
		}
		return down
	}
	if err := r.s.d.FS.Remove(paths.DrillRecord(team)); err != nil {
		return errors.Join(copyErr, fmt.Errorf("team %s serves again, but %s could not be removed: %w", team, paths.DrillRecord(team), err))
	}
	return nil
}

// read is drill.json, by the trust rule as root's own file.
func (r *drilling) read() (DrillRecord, bool, error) {
	raw, found, err := state.ReadOwnedFile(r.s.d.Root+paths.DrillRecord(r.s.team), maxRecord, r.s.d.StateOwnerUID)
	if err != nil || !found {
		return DrillRecord{}, false, err
	}
	rec, err := DecodeDrillRecord(raw, r.s.team)
	return rec, err == nil, err
}
