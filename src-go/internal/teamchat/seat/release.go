package seat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// releaseWait bounds a release's wait for the session's seat lock (C 3.7.3 release step 1).
const releaseWait = 60 * time.Second

// Release asks for one seat a session holds to be released.
type Release struct {
	Seat    Seat
	Session paths.SessionName
	// Leaving is a leave or a remove: the session goes on, so no disconnected notice is due.
	Leaving bool
}

// Notice is what a disconnected notice is posted from (C 3.7.5): the pool's held/ bundle,
// since_ms from held.json, and the ended run's id from the free seat lock.
type Notice struct {
	Bundle  string
	SinceMS int64
	Run     paths.RunID
}

// Outcome is a notice judged: posted (or a goodbye found), or not possible for good and why.
type Outcome struct {
	Posted bool
	Why    string
}

// Announcer posts the disconnected notice as the subject (area B's `seat disconnected`). An
// error is a transient failure: the release stops and the next one retries.
type Announcer interface {
	Disconnected(ctx context.Context, n Notice) (Outcome, error)
}

// locker runs f under the allocation lock: by taking it, or inside a take that holds it.
type locker func(ctx context.Context, f func() error) error

// Release releases a seat, as the owner (C 3.7.3 "Releasing a seat", SX1-SX6): the waits and
// the homeserver calls run without the allocation lock, which it takes for steps 2 and 5 only.
// A release that stopped resumes where it stopped; a seat with no held/ is done.
func (s *Store) Release(ctx context.Context, r Release) error {
	return s.release(ctx, r, s.underLock)
}

// underLock runs f holding the store's allocation lock.
func (s *Store) underLock(ctx context.Context, f func() error) error {
	a, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	return errors.Join(f(), a.Unlock())
}

// alreadyLocked runs f in a take, which holds the allocation lock (n2c-D4).
func alreadyLocked(_ context.Context, f func() error) error { return f() }

func (s *Store) release(ctx context.Context, r Release, locked locker) error {
	pool := s.store.PoolSeat(s.checkout, r.Seat.Team, r.Seat.Name)
	h, rec, err := s.heldRecord(ctx, r.Seat, pool, locked)
	if err != nil || h == nil {
		return err
	}
	return errors.Join(s.releaseHeld(ctx, r, locked, pool, h, rec), h.close())
}

// releaseHeld is steps 1 to 5 for the holding h, held/ opened by fd: steps 3 and 4 read and
// write through h, so a take that releases this holding itself and makes a new held/ meanwhile
// is never touched, and step 5 is done by whoever finds the claim still in place.
func (s *Store) releaseHeld(ctx context.Context, r Release, locked locker, pool paths.PoolSeat, h *dir, rec HeldRecord) error {
	if rec.Session != r.Session {
		return fmt.Errorf("%w: %s by session %s, not %s", ErrHeld, r.Seat.Name, rec.Session, r.Session)
	}
	seat := s.store.SessionSeat(r.Session, r.Seat.Team)
	moved := s.store.ReleasedSession(r.Session, r.Seat.Team, rec.Claim)
	another, err := s.copyOfAnother(seat, rec.Claim)
	if err != nil {
		return err
	}
	var lock lockProbe
	present := false
	if !another {
		if lock, present, err = s.awaitFree(ctx, r, seat); err != nil {
			return err
		}
		if err = locked(ctx, func() error { return s.moveSessionSeat(seat, moved) }); err != nil {
			return err
		}
	}
	if !r.Leaving {
		if err = s.noticeStep(ctx, h, pool, rec, lock, present, moved); err != nil {
			return err
		}
	}
	if err = s.retire(ctx, h, pool); err != nil {
		return err
	}
	return locked(ctx, func() error { return s.finish(r.Seat, pool, rec.Claim) })
}

// copyOfAnother is whether the session seat directory holds another holding's complete copy:
// a release resumed after step 2, once its session took another seat of the team into the same
// directory (n2c-D12). Steps 1 and 2 are then done for this holding.
func (s *Store) copyOfAnother(seat paths.Seat, claim paths.ClaimID) (bool, error) {
	data, err := s.root.read(seat.Bundle(), maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	b, decErr := DecodeHeldBundle(data)
	return decErr == nil && b.Claim != claim, nil
}

// moveSessionSeat is step 2: the session's seat directory, whatever is there and never
// followed, renamed into quarantine; already gone goes on.
func (s *Store) moveSessionSeat(seat, moved paths.Seat) error {
	err := s.root.rename(seat.Dir(), moved.Dir(), unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

// noticeStep is step 3 for a session that is over. A release resumed after step 2 reads the
// ended run from the quarantined seat lock, by the same probe (SD4, n2c-D5).
func (s *Store) noticeStep(ctx context.Context, h *dir, pool paths.PoolSeat, rec HeldRecord, lock lockProbe, present bool, moved paths.Seat) error {
	if !present {
		var err error
		if lock, err = s.probe(moved); err != nil {
			return err
		}
	}
	return s.announce(ctx, h, pool, rec, lock)
}

// heldRecord is held/, opened by fd (the caller closes it), and its held.json; h is nil when
// there is no held/. A held/ with no complete held.json is judged again under the lock, since a
// take holds the lock from making held/ to writing held.json: still incomplete, it is a take
// that died at step 2 and goes to quarantine, any token in it unlinked.
func (s *Store) heldRecord(ctx context.Context, seat Seat, pool paths.PoolSeat, locked locker) (*dir, HeldRecord, error) {
	h, rec, complete, err := s.openHeld(pool)
	if err != nil || h == nil || complete {
		return h, rec, err
	}
	if err, h = h.close(), nil; err != nil {
		return nil, HeldRecord{}, err
	}
	err = locked(ctx, func() error {
		h, rec, complete, err = s.openHeld(pool)
		if err != nil || h == nil || complete {
			return err
		}
		err, h = h.close(), nil
		if err != nil {
			return err
		}
		return s.quarantine(seat, pool.Held(), filepath.Base(pool.HeldToken()))
	})
	if err != nil && h != nil {
		return nil, HeldRecord{}, errors.Join(err, h.close())
	}
	return h, rec, err
}

// openHeld opens held/ and reads its held.json through that fd; h is nil when there is no
// held/, and complete is false when held.json is missing or does not decode.
func (s *Store) openHeld(pool paths.PoolSeat) (*dir, HeldRecord, bool, error) {
	h, err := s.root.sub(pool.Held())
	if errors.Is(err, unix.ENOENT) {
		return nil, HeldRecord{}, false, nil
	}
	if err != nil {
		return nil, HeldRecord{}, false, err
	}
	data, err := h.read(pool.HeldRecord(), maxRecord)
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return nil, HeldRecord{}, false, errors.Join(err, h.close())
	}
	rec, decErr := decodeHeldRecord(data)
	return h, rec, err == nil && decErr == nil, nil
}

// awaitFree is step 1: unless the session's seat directory is gone (present false), wait at
// most releaseWait for its seat lock to be free or the session to be over.
func (s *Store) awaitFree(ctx context.Context, r Release, seat paths.Seat) (lockProbe, bool, error) {
	present, err := s.root.exists(seat.Dir())
	if err != nil || !present {
		return lockProbe{}, false, err
	}
	deadline := s.clock.Now().Add(releaseWait)
	for {
		lock, err := s.probe(seat)
		if err != nil || lock.state != lockHeld {
			return lock, true, err
		}
		restorable, err := s.sessions.Restorable(r.Session)
		if err != nil {
			return lockProbe{}, true, fmt.Errorf("seat: the record of session %s: %w", r.Session, err)
		}
		if !restorable {
			return lock, true, nil
		}
		if !s.clock.Now().Before(deadline) {
			return lockProbe{}, true, fmt.Errorf("%w: the seat lock of %s in session %s is held", ErrBusy, r.Seat.Name, r.Session)
		}
		if err := s.clock.Sleep(ctx, allocPoll); err != nil {
			return lockProbe{}, true, err
		}
	}
}

// Announce is the crashed-session path (SD3): a free seat lock naming a run is announced once,
// recorded in held/notice.json, and the seat stays held. A held lock is "not yet", not an error.
func (s *Store) Announce(ctx context.Context, seat Seat, session paths.SessionName) error {
	pool := s.store.PoolSeat(s.checkout, seat.Team, seat.Name)
	h, rec, err := s.heldRecord(ctx, seat, pool, s.underLock)
	if err != nil || h == nil {
		return err
	}
	return errors.Join(s.announceHeld(ctx, seat, session, pool, h, rec), h.close())
}

func (s *Store) announceHeld(ctx context.Context, seat Seat, session paths.SessionName, pool paths.PoolSeat, h *dir, rec HeldRecord) error {
	if rec.Session != session {
		return fmt.Errorf("%w: %s by session %s, not %s", ErrHeld, seat.Name, rec.Session, session)
	}
	lock, err := s.probe(s.store.SessionSeat(session, seat.Team))
	if err != nil || lock.state != lockFree {
		return err
	}
	return s.announce(ctx, h, pool, rec, lock)
}

// announce is step 3 (SD1, SD2): a free lock that names a run with no launch failure, not
// already recorded for this claim, is asked of the announcer and its outcome recorded in the
// holding h, which is this claim's whatever has happened to held/ since.
func (s *Store) announce(ctx context.Context, h *dir, pool paths.PoolSeat, rec HeldRecord, lock lockProbe) error {
	run, ok := runOf(lock)
	if !ok {
		return nil
	}
	data, err := h.read(pool.HeldNotice(), maxRecord)
	if err == nil {
		if prev, decErr := decodeNoticeRecord(data); decErr == nil && prev.Claim == rec.Claim && prev.Run == run {
			return nil
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	out, err := s.announcer.Disconnected(ctx, Notice{Bundle: pool.Held(), SinceMS: rec.SinceMS, Run: run})
	if err != nil {
		return fmt.Errorf("seat: the disconnected notice for %s: %w", pool.Dir(), err)
	}
	data, err = encodeRecord(NoticeRecord{V: recordVersion, Claim: rec.Claim, Run: run, Posted: out.Posted, Why: out.Why})
	if err != nil {
		return err
	}
	if _, err := decodeNoticeRecord(data); err != nil {
		return fmt.Errorf("seat: the announcer's outcome: %w", err)
	}
	return h.replace(pool.HeldNotice(), data)
}

// seatLockRecord is B 2.5's seat lock record.
type seatLockRecord struct {
	V              int         `json:"v"`
	Run            paths.RunID `json:"run"`
	StartedMS      int64       `json:"started_ms"`
	LaunchFailedMS *int64      `json:"launch_failed_ms,omitempty"`
}

// runOf is the ended run a free seat lock names (SD1): none when the lock is missing or its
// record is empty, undecodable, of a run id that fails its grammar, or of a failed launch.
func runOf(lock lockProbe) (paths.RunID, bool) {
	if lock.state != lockFree || len(lock.record) == 0 {
		return paths.RunID{}, false
	}
	var rec seatLockRecord
	if err := decodeStrict(lock.record, []string{"v", "run", "started_ms"}, &rec); err != nil {
		return paths.RunID{}, false
	}
	if rec.V != recordVersion || rec.LaunchFailedMS != nil {
		return paths.RunID{}, false
	}
	return rec.Run, true
}

// retire is step 4: the credential's Retire with the holding h's token; one with no token has
// nothing to retire.
func (s *Store) retire(ctx context.Context, h *dir, pool paths.PoolSeat) error {
	data, err := h.read(pool.HeldToken(), maxSecret)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := ParseSecret(string(data))
	if err != nil {
		return fmt.Errorf("%s: %w", pool.HeldToken(), err)
	}
	bundleData, err := s.root.read(pool.Bundle(), maxRecord)
	if err != nil {
		return err
	}
	b, err := DecodeBundle(bundleData)
	if err != nil {
		return fmt.Errorf("%s: %w", pool.Bundle(), err)
	}
	return s.credential.Retire(ctx, b, token)
}

// finish is step 5: released.json for this claim and now, then held/ moved to quarantine and
// its token unlinked. The pool's credential and seat.json are untouched. A held/ that is gone or
// carries another claim was released by another (a take, n2c-D4): this release is done.
func (s *Store) finish(seat Seat, pool paths.PoolSeat, claim paths.ClaimID) error {
	current, err := s.root.read(pool.HeldRecord(), maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	rec, decErr := decodeHeldRecord(current)
	if stillThisClaim := decErr == nil && rec.Claim == claim; !stillThisClaim {
		return nil
	}
	data, err := encodeRecord(ReleasedRecord{V: recordVersion, Claim: claim, ReleasedMS: s.clock.Now().UnixMilli()})
	if err != nil {
		return err
	}
	if err := s.root.replace(pool.ReleasedRecord(), data); err != nil {
		return err
	}
	to := s.store.ReleasedPool(s.checkout, seat.Team, seat.Name, claim)
	return s.moveToQuarantine(pool.Held(), to, filepath.Base(pool.HeldToken()))
}
