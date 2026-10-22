package seat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The refusals of a take (C 3.7.3 take step 1).
var (
	// ErrHeld is a seat whose held/ names a session that is live or restorable.
	ErrHeld = errors.New("seat: the seat is held")
	// ErrReturn is a parked or lost handle: `member return` places it again.
	ErrReturn = errors.New("seat: the handle needs member return")
	// ErrRetired is a retired handle; its pool directory goes to quarantine.
	ErrRetired = errors.New("seat: the handle is retired")
	// ErrIncomplete is a pool seat without its credential or seat.json.
	ErrIncomplete = errors.New("seat: the pool record is incomplete")
	// ErrLeftover is a `<seat>.new-*` left by a placement that died; `member reissue` mends it.
	ErrLeftover = errors.New("seat: a placement left a claimed directory")
)

// HandleState is the handle's state in the status record's roster, which decides a take.
type HandleState int

// The handle states a take is given.
const (
	StateActive HandleState = iota + 1
	StateParked
	StateLost
	StateNeedsReissue
	StateRetired
)

// Take asks for one pool seat for one session.
type Take struct {
	Seat    Seat
	Session paths.SessionName
	State   HandleState
}

// Taken is a seat taken: the take's claim and the session's seat directory.
type Taken struct {
	Claim paths.ClaimID
	Dir   string
}

// Take takes a pool seat for a session, as the owner, under the store's allocation lock (C 3.7.3
// "Taking a seat for a session", ST1-ST7). A ccy session and a host cc session take the same
// way, into the session's seat directory (n2c-D3).
func (s *Store) Take(ctx context.Context, a *Alloc, t Take) (Taken, error) {
	if !s.holds(a) {
		return Taken{}, ErrNotLocked
	}
	pool := s.store.PoolSeat(s.checkout, t.Seat.Team, t.Seat.Name)
	if err := s.checkState(ctx, t, pool); err != nil {
		return Taken{}, err
	}
	bundle, err := s.poolRecord(t.Seat, pool)
	if err != nil {
		return Taken{}, err
	}
	if err = s.clearHeld(ctx, t.Seat, pool); err != nil {
		return Taken{}, err
	}
	seat := s.store.SessionSeat(t.Session, t.Seat.Team)
	if err = s.refuseSessionSeat(seat); err != nil {
		return Taken{}, err
	}
	claim, err := newClaim()
	if err != nil {
		return Taken{}, err
	}
	if err = s.hold(pool, t.Session, claim); err != nil {
		return Taken{}, err
	}
	heldBundle, token, err := s.mint(ctx, pool, bundle, claim)
	if err != nil {
		return Taken{}, err
	}
	if err := s.copyToSession(s.store.Session(t.Session), seat, token, heldBundle); err != nil {
		return Taken{}, err
	}
	return Taken{Claim: claim, Dir: seat.Dir()}, nil
}

// checkState is step 1's handle state; a retired seat's pool directory goes to quarantine
// under a fresh claim id, its credential unlinked (n2c-D2), once its held/ is cleared as a take
// clears it: a live holding is ErrHeld, an ended one is released first, so no live token goes
// to quarantine.
func (s *Store) checkState(ctx context.Context, t Take, pool paths.PoolSeat) error {
	switch t.State {
	case StateActive:
		return nil
	case StateParked, StateLost:
		return fmt.Errorf("%w: %s", ErrReturn, t.Seat.Name)
	case StateNeedsReissue:
		return fmt.Errorf("%w: %s is marked needs: reissue", ErrReissue, t.Seat.Name)
	case StateRetired:
		if err := s.clearHeld(ctx, t.Seat, pool); err != nil {
			return err
		}
		if err := s.quarantine(t.Seat, pool.Dir(), s.credentialName(t.Seat)); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s, its pool record moved to quarantine", ErrRetired, t.Seat.Name)
	}
	return fmt.Errorf("seat: unknown handle state %d", t.State)
}

// poolRecord is step 1's complete record: no leftover claim, the credential file and a
// seat.json naming this seat.
func (s *Store) poolRecord(seat Seat, pool paths.PoolSeat) (Bundle, error) {
	if err := s.refuseLeftovers(seat); err != nil {
		return Bundle{}, err
	}
	for _, path := range []string{s.credentialPath(pool), pool.Bundle()} {
		found, err := s.root.exists(path)
		if err != nil {
			return Bundle{}, err
		}
		if !found {
			return Bundle{}, fmt.Errorf("%w: %s is missing", ErrIncomplete, path)
		}
	}
	data, err := s.root.read(pool.Bundle(), maxRecord)
	if err != nil {
		return Bundle{}, err
	}
	b, err := DecodeBundle(data)
	if err != nil {
		return Bundle{}, fmt.Errorf("%s: %w", pool.Bundle(), err)
	}
	if b.Team != seat.Team || b.Handle().String() != seat.Name.String() {
		return Bundle{}, invalid(pool.Bundle() + " names another seat")
	}
	return b, nil
}

// refuseLeftovers is ErrLeftover naming any `<seat>.new-*` in the seat's team directory.
func (s *Store) refuseLeftovers(seat Seat) error {
	names, err := s.leftovers(seat)
	if err != nil || len(names) == 0 {
		return err
	}
	team := s.store.PoolTeam(s.checkout, seat.Team)
	return fmt.Errorf("%w: %s/%s; run member reissue for %s", ErrLeftover, team, names[0], seat.Name)
}

// leftovers are the `<seat>.new-*` names in the seat's team directory.
func (s *Store) leftovers(seat Seat) ([]string, error) {
	names, err := s.root.entries(s.store.PoolTeam(s.checkout, seat.Team))
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range names {
		if strings.HasPrefix(name, seat.Name.String()+".new-") {
			out = append(out, name)
		}
	}
	return out, nil
}

// clearHeld is step 1's held/: none goes on; one with no complete held.json goes to
// quarantine; one naming a live or restorable session is ErrHeld; one naming a session that
// is over is released first, under the lock this take holds (n2c-D4).
func (s *Store) clearHeld(ctx context.Context, seat Seat, pool paths.PoolSeat) error {
	h, rec, err := s.heldRecord(ctx, seat, pool, alreadyLocked)
	if err != nil || h == nil {
		return err
	}
	if err = h.close(); err != nil {
		return err
	}
	held, err := s.sessionHolds(rec.Session, seat.Team)
	if err != nil {
		return err
	}
	if held {
		return fmt.Errorf("%w: by session %s", ErrHeld, rec.Session)
	}
	err = s.release(ctx, Release{Seat: seat, Session: rec.Session}, alreadyLocked)
	if errors.Is(err, ErrCopyKept) {
		// The ended holding is released, its token retired; the copy its container kept is
		// that session's, and never stops another session's take (n2c-D14).
		return nil
	}
	return err
}

// sessionHolds is whether a session holds its seat: its seat lock is held, or it is restorable.
// What decides is the host's: held/ and the session record. The lock only adds a live keeper,
// so a lock its container made refused, like a deleted one, holds nothing (n2c-D14).
func (s *Store) sessionHolds(session paths.SessionName, team paths.TeamID) (bool, error) {
	got, err := s.probe(s.store.SessionSeat(session, team))
	if err != nil {
		return false, err
	}
	if got.state == lockHeld {
		return true, nil
	}
	restorable, err := s.sessions.Restorable(session)
	if err != nil {
		return false, fmt.Errorf("seat: the record of session %s: %w", session, err)
	}
	return restorable, nil
}

// refuseSessionSeat is step 1's session seat directory: anything there already, or a session
// directory the owner cannot look into, is ErrHeld naming it, before held/ is made, so at most
// one holding of a session and team has a copy (n2c-D12). It is looked at, never opened or
// followed; a member that plants one stops only its own session's take, and no token is minted.
func (s *Store) refuseSessionSeat(seat paths.Seat) error {
	found, err := s.root.exists(seat.Dir())
	if memberRefused(err) {
		return fmt.Errorf("%w: %s cannot be looked into: %w", ErrHeld, seat.Dir(), err)
	}
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("%w: %s is there already", ErrHeld, seat.Dir())
	}
	return nil
}

// hold is step 2: held/ made, and held.json with the claim and now.
func (s *Store) hold(pool paths.PoolSeat, session paths.SessionName, claim paths.ClaimID) error {
	if err := s.root.mkdirFresh(pool.Held()); err != nil {
		return err
	}
	data, err := encodeRecord(HeldRecord{V: recordVersion, Session: session, Claim: claim, SinceMS: s.clock.Now().UnixMilli()})
	if err != nil {
		return err
	}
	return s.root.create(pool.HeldRecord(), data)
}

// mint is steps 3 and 4: the credential's token, then held/token and held/seat.json. A refused
// login removes held/; any other failure leaves it naming a session that holds nothing.
func (s *Store) mint(ctx context.Context, pool paths.PoolSeat, b Bundle, claim paths.ClaimID) ([]byte, Secret, error) {
	h := HeldBundle{Bundle: b, Claim: claim}
	releasedMS, released, err := s.releasedMS(pool)
	if err != nil {
		return nil, Secret{}, err
	}
	if released {
		h.ReleasedMS = &releasedMS
	}
	heldBundle, err := h.Encode()
	if err != nil {
		return nil, Secret{}, err
	}
	secret, err := s.poolSecret(pool)
	if err != nil {
		return nil, Secret{}, err
	}
	token, err := s.credential.Mint(ctx, b, secret)
	if errors.Is(err, ErrReissue) {
		return nil, Secret{}, errors.Join(err, s.root.unlink(pool.HeldRecord()), s.root.rmdir(pool.Held()))
	}
	if err != nil {
		return nil, Secret{}, err
	}
	if err := s.root.create(pool.HeldToken(), []byte(token.Reveal())); err != nil {
		return nil, Secret{}, err
	}
	return heldBundle, token, s.root.create(pool.HeldBundle(), heldBundle)
}

// releasedMS is the seat's last release time from released.json; false when it has none.
func (s *Store) releasedMS(pool paths.PoolSeat) (int64, bool, error) {
	data, err := s.root.read(pool.ReleasedRecord(), maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	rec, err := decodeReleasedRecord(data)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %w", pool.ReleasedRecord(), err)
	}
	return rec.ReleasedMS, true, nil
}

// poolSecret is the pool's credential file, read as SW3 reads a record.
func (s *Store) poolSecret(pool paths.PoolSeat) (Secret, error) {
	path := s.credentialPath(pool)
	data, err := s.root.read(path, maxSecret)
	if err != nil {
		return Secret{}, err
	}
	secret, err := ParseSecret(string(data))
	if err != nil {
		return Secret{}, fmt.Errorf("%s: %w", path, err)
	}
	return secret, nil
}

// copyToSession is steps 5 to 7: sessions/<session>/ made 0700 when missing, the seat's
// directory made fresh, opened and checked, then token and seat.json written through the fd
// that was checked.
func (s *Store) copyToSession(session string, seat paths.Seat, token Secret, heldBundle []byte) error {
	if err := s.root.mkdirAll(session); err != nil {
		return err
	}
	if err := s.root.mkdirFresh(seat.Dir()); err != nil {
		return err
	}
	d, err := s.root.sub(seat.Dir())
	if err != nil {
		return err
	}
	err = d.emptyOwned()
	if err == nil {
		err = d.create(seat.Token(), []byte(token.Reveal()))
	}
	if err == nil {
		err = d.create(seat.Bundle(), heldBundle)
	}
	return errors.Join(err, d.close())
}

// credentialPath is the pool file in p the credential keeps its secret in.
func (s *Store) credentialPath(p paths.PoolSeat) string {
	if s.credential.Kind() == KindToken {
		return p.Token()
	}
	return p.Password()
}

// credentialName is the name of the credential's pool file, unlinked when a record goes to
// quarantine.
func (s *Store) credentialName(seat Seat) string {
	return filepath.Base(s.credentialPath(s.store.PoolSeat(s.checkout, seat.Team, seat.Name)))
}

// quarantine moves a pool seat's directory to released/pool/ under a fresh claim id, then
// unlinks the named file in it, when one is named and there.
func (s *Store) quarantine(seat Seat, from, unlinkName string) error {
	claim, err := newClaim()
	if err != nil {
		return err
	}
	to := s.store.ReleasedPool(s.checkout, seat.Team, seat.Name, claim)
	return s.moveToQuarantine(from, to, unlinkName)
}

// moveToQuarantine renames from as to, never replacing, then unlinks the named file in it.
func (s *Store) moveToQuarantine(from, to, unlinkName string) error {
	if err := s.root.rename(from, to, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if unlinkName == "" {
		return nil
	}
	if err := s.root.unlink(to + "/" + unlinkName); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return nil
}

// newClaim is a fresh claim id.
func newClaim() (paths.ClaimID, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return paths.ClaimID{}, err
	}
	return paths.ParseClaimID(hex.EncodeToString(b[:]))
}
