package seat

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrRunning is a seat whose member link is running for its team, or whose link's lock the
// owner cannot look at: an abandon refuses it.
var ErrRunning = errors.New("seat: its member link is running for its team")

// Abandoned is what an abandon did: Held is false for a seat no session held (nothing done),
// and Session is the session that held it.
type Abandoned struct {
	Held    bool
	Session paths.SessionName
}

// Abandon releases a held seat without its logout, on the owner's word (issue 386): for a seat
// whose server will never answer its logout again, so a release could never finish. It makes no
// call to any server. Under the allocation lock it does a release's steps 2 and 5 only: the
// session's seat directory moved to quarantine as a release moves it, then released.json with
// the abandonment (reason, by, at_ms) and held/ moved to quarantine, its token unlinked. No
// disconnected notice is posted.
//
// The check that the seat's link is not running is per team: the session's seat lock of the
// seat's team (the lock its member link holds while it runs) must be free or missing, whatever
// the session's seats of other teams are doing. A lock the owner cannot look at is refused too.
// A seat that is not held is nothing to do. The caller judges whether the team is gone.
func (s *Store) Abandon(ctx context.Context, seat Seat, reason, by string) (Abandoned, error) {
	if err := (Abandonment{Reason: reason, By: by}).Check(); err != nil {
		return Abandoned{}, err
	}
	pool := s.store.PoolSeat(s.checkout, seat.Team, seat.Name)
	h, rec, err := s.heldRecord(ctx, seat, pool, s.underLock)
	if err != nil || h == nil {
		return Abandoned{}, err
	}
	if err = h.close(); err != nil {
		return Abandoned{}, err
	}
	out := Abandoned{Session: rec.Session}
	var kept bool
	err = s.underLock(ctx, func() error {
		var lockedErr error
		out.Held, kept, lockedErr = s.abandonHeld(seat, pool, rec, Abandonment{Reason: reason, By: by})
		return lockedErr
	})
	if err != nil {
		return Abandoned{Session: rec.Session}, err
	}
	if kept {
		return out, fmt.Errorf("%w: %s", ErrCopyKept, s.store.SessionSeat(rec.Session, seat.Team).Dir())
	}
	return out, nil
}

// abandonHeld is the abandon under the allocation lock: held is false when held/ is gone or
// carries another claim than rec's (released meanwhile), kept is a session seat directory its
// container made the owner unable to move (the release goes on, as a release does).
func (s *Store) abandonHeld(seat Seat, pool paths.PoolSeat, rec HeldRecord, ab Abandonment) (held, kept bool, err error) {
	current, err := s.root.read(pool.HeldRecord(), maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if !holdsClaim(current, rec.Claim) {
		return false, false, nil
	}
	copySeat := s.store.SessionSeat(rec.Session, seat.Team)
	lock, err := s.probe(copySeat)
	if err != nil {
		return false, false, err
	}
	switch lock.state {
	case lockHeld:
		return false, false, fmt.Errorf("%w: seat %s of team %s, session %s holds %s", ErrRunning, seat.Name, seat.Team, rec.Session, copySeat.Lock())
	case lockRefused:
		return false, false, fmt.Errorf("%w: seat %s of team %s, session %s: %s cannot be looked at, so its link may be running", ErrRunning, seat.Name, seat.Team, rec.Session, copySeat.Lock())
	case lockMissing, lockFree:
	}
	moved := s.store.ReleasedSession(rec.Session, seat.Team, rec.Claim)
	moveDone, err := s.root.exists(moved.Dir())
	if err != nil {
		return false, false, err
	}
	if !moveDone {
		own, ownErr := s.mayOwnCopy(Release{Seat: seat, Session: rec.Session}, rec)
		if ownErr != nil {
			return false, false, ownErr
		}
		if own {
			if kept, err = s.moveSessionSeat(copySeat, moved); err != nil {
				return false, false, err
			}
		}
	}
	ab.AtMS = s.clock.Now().UnixMilli()
	return true, kept, s.finish(seat, pool, rec.Claim, &ab)
}

// holdsClaim is whether held.json's data decodes and names claim: one that does not is
// another holding's, or an incomplete one, never this abandon's.
func holdsClaim(data []byte, claim paths.ClaimID) bool {
	rec, err := decodeHeldRecord(data)
	return err == nil && rec.Claim == claim
}
