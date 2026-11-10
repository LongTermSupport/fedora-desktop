package health

import (
	"context"
	"errors"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// Leaves renews a team's leaf by apply's own step (host.LeafRenewer on the host), under the team
// lock its caller holds: made again by X14's rule, and the terminator restarted to read it.
type Leaves interface {
	Renew(ctx context.Context, team paths.TeamID, run host.Terminator) (host.LeafRenewal, error)
}

// The events of a renewal: the leaf made, with apply's line for why after EventLeafPrefix, and
// the terminator restarted.
const (
	EventLeafPrefix    = "the TLS "
	EventLeafRestarted = "the TLS terminator restarted to read its leaf"
)

// leaf is X31: the team's leaf renewed with no apply, so a team not applied for a year keeps a
// leaf members trust. It runs under the team lock taken without waiting (a busy lock is a holder
// that owns the team, and the next tick tries again), never for a caller holding the lock
// (Refresh), and never while a restore is recorded (HB3). A renewal that fails is a problem on the
// record, tried again at every tick: while the leaf is valid it names the date it ends, and a leaf
// past its end is a problem of its own.
func (t *Tick) leaf(ctx context.Context, team Team, f *facts, run UnitState, read bool) error {
	if f.held {
		return nil
	}
	return t.underLock(ctx, team, f, func() error {
		got, err := t.cfg.Leaves.Renew(ctx, team.ID, host.Terminator{Active: read && run.Active == unitActive, Since: run.ActiveSince})
		at := t.cfg.Clock.Now()
		if got.Made {
			f.events = append(f.events, status.Event{AtMS: at.UnixMilli(), What: EventLeafPrefix + got.Why})
		}
		if got.Restarted {
			f.events = append(f.events, status.Event{AtMS: at.UnixMilli(), What: EventLeafRestarted})
		}
		if err != nil {
			f.problems = append(f.problems, leafProblem(team.ID, got.NotAfter, at, err))
		}
		return nil
	})
}

// leafProblem is a renewal that failed, by what is known of the leaf in force. Its cause is the
// fixed form host.LeafError gives, never the error's own text, which the record could refuse.
func leafProblem(team paths.TeamID, notAfter, now time.Time, err error) status.Problem {
	fix := "teamchat-admin health " + team.String() + ", then apply the team again (" + paths.PlayFile + ")"
	cause := "the renewal failed"
	if le, ok := errors.AsType[*host.LeafError](err); ok {
		cause = le.Cause
	}
	date := notAfter.UTC().Format(time.DateOnly)
	switch {
	case notAfter.IsZero():
		return status.Problem{What: "the TLS leaf cannot be judged: " + cause, Fix: fix}
	case now.Before(notAfter):
		return status.Problem{What: "the TLS leaf could not be made again; it expires on " + date + ": " + cause, Fix: fix}
	}
	return status.Problem{What: "the TLS leaf expired on " + date + ": no member can reach the homeserver; it could not be made again: " + cause, Fix: fix}
}

// underLock runs do under the team lock taken without waiting, once the stamp and the restore
// record are read again: a busy lock means its holder owns the team and nothing is done, a team
// removed meanwhile is errRemoved, and a restore recorded meanwhile is shown and nothing is done
// (HB3).
func (t *Tick) underLock(ctx context.Context, team Team, f *facts, do func() error) error {
	lock, err := t.cfg.Locks.TryLock(ctx, team.ID)
	if err != nil {
		if isBusy(err) {
			return nil
		}
		return err
	}
	err = t.checked(ctx, team, f, do)
	if releaseErr := lock.Release(); err == nil {
		err = releaseErr
	}
	return err
}

func (t *Tick) checked(ctx context.Context, team Team, f *facts, do func() error) error {
	stamped, err := t.cfg.Teams.Stamp(ctx, team.ID)
	if err != nil {
		return err
	}
	if !stamped {
		return errRemoved
	}
	if err = t.restore(ctx, team.ID, f); err != nil || f.restore != nil {
		return err
	}
	return do()
}
