package health

import (
	"context"
	"fmt"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The probe's bounds (C 3.11.2 step 2).
const (
	StartGrace = 60 * time.Second
	ProbeGap   = 10 * time.Second
	Misses     = 3
)

// The events the tick writes.
const (
	EventRestarted   = "restarted after three missed probes"
	EventSelfRestart = "the homeserver unit restarted by itself (%d restarts)"
	EventByHand      = "restarted by hand"
)

// UnitState is a service unit's state over D-Bus (step 1), only properties a service has.
type UnitState struct {
	Active, Sub, InvocationID string
	NRestarts                 int64
	// ActiveSince is when the unit entered active; zero when it is not.
	ActiveSince time.Time
}

// Units is systemd: a unit's state, and its restart.
type Units interface {
	State(ctx context.Context, unit string) (UnitState, error)
	Restart(ctx context.Context, unit string) error
}

// Prober probes a team's /_matrix/client/versions on loopback; nil is an answer.
type Prober interface {
	Probe(ctx context.Context, team paths.TeamID) error
}

// homeserver is steps 1 and 2 (HT2).
func (t *Tick) homeserver(ctx context.Context, team Team, f *facts) error {
	unit := paths.ServerUnit(team.ID)
	state, read := t.unitState(ctx, unit)
	if !read {
		f.unit, f.probe = UnitState{Active: "inactive"}, status.ProbeNotRun
		f.problems = append(f.problems, status.Problem{What: "the homeserver unit's state cannot be read", Fix: "systemctl status " + unit})
		return nil
	}
	f.unit = state
	t.restarts(f)
	if state.Active != unitActive {
		f.probe = status.ProbeNotRun
		f.problems = append(f.problems, status.Problem{What: "the homeserver unit is " + state.Active, Fix: "systemctl status " + unit})
		return nil
	}
	f.probe = status.ProbeOK
	if t.cfg.Prober.Probe(ctx, team.ID) == nil {
		return nil
	}
	f.probe = status.ProbeMissed
	if t.cfg.Clock.Now().Sub(state.ActiveSince) < StartGrace {
		return nil
	}
	answered, err := t.probeAgain(ctx, team.ID, unit, state)
	if err != nil || answered {
		if answered {
			f.probe = status.ProbeOK
		}
		return err
	}
	return t.restart(ctx, team, unit, f)
}

// unitActive is the ActiveState of a running unit.
const unitActive = "active"

// unitState is the unit's state; read is false when systemd could not say, which the record
// shows as a problem rather than failing the team's tick.
func (t *Tick) unitState(ctx context.Context, unit string) (UnitState, bool) {
	state, err := t.cfg.Units.State(ctx, unit)
	return state, err == nil
}

// restarts is a rise in NRestarts over the last record's, one event, or a fall, a manual
// restart (n2e-D4).
func (t *Tick) restarts(f *facts) {
	if f.prev == nil {
		return
	}
	at := t.cfg.Clock.Now().UnixMilli()
	switch was := f.prev.Homeserver.NRestarts; {
	case f.unit.NRestarts > was:
		f.events = append(f.events, status.Event{AtMS: at, What: fmt.Sprintf(EventSelfRestart, f.unit.NRestarts)})
	case f.unit.NRestarts < was:
		f.events = append(f.events, status.Event{AtMS: at, What: EventByHand})
	}
}

// probeAgain is the second and third probe, ProbeGap apart, each while the same invocation is
// still active; answered is any of them answering, and a changed invocation counts as one.
func (t *Tick) probeAgain(ctx context.Context, team paths.TeamID, unit string, first UnitState) (bool, error) {
	for range Misses - 1 {
		if err := t.cfg.Clock.Sleep(ctx, ProbeGap); err != nil {
			return false, err
		}
		now, read := t.unitState(ctx, unit)
		if !read || now.Active != unitActive || now.InvocationID != first.InvocationID {
			return true, nil
		}
		if t.cfg.Prober.Probe(ctx, team) == nil {
			return true, nil
		}
	}
	return false, nil
}

// restart restarts the unit under the team lock taken without waiting, and writes its event
// at once with seq raised; a busy lock means the holder owns the team, so nothing is done.
func (t *Tick) restart(ctx context.Context, team Team, unit string, f *facts) error {
	lock, err := t.cfg.Locks.TryLock(team.ID)
	if err != nil {
		if isBusy(err) {
			return nil
		}
		return err
	}
	err = t.restartLocked(ctx, team, unit, f)
	if unlockErr := lock.Unlock(); err == nil {
		err = unlockErr
	}
	return err
}

func (t *Tick) restartLocked(ctx context.Context, team Team, unit string, f *facts) error {
	if err := t.cfg.Units.Restart(ctx, unit); err != nil {
		return fmt.Errorf("health: restarting %s: %w", unit, err)
	}
	path := t.cfg.StatusPath(team.ID)
	var prev *status.Record
	if rec, found, err := status.Read(path, team.ID); err == nil && found {
		prev = &rec
	}
	rec := t.base(team, prev)
	if prev == nil {
		rec.Homeserver = status.Homeserver{Unit: f.unit.Active, Probe: status.ProbeMissed}
		rec.Backup.LastRun = status.RunNone
	}
	rec.Events = status.KeepEvents(append(rec.Events, status.Event{AtMS: t.cfg.Clock.Now().UnixMilli(), What: EventRestarted}), t.cfg.Clock.Now())
	if err := status.Write(path, rec, t.cfg.GID); err != nil {
		return err
	}
	f.prev, f.seq, f.restarted = &rec, rec.Seq, true
	return nil
}
