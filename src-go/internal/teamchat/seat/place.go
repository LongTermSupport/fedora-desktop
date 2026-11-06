package seat

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Claim is a claimed `<seat>.new-<claim>` directory a placement fills (C 3.7.3 "How it is
// placed", SP1, SP2), made under the allocation lock it is filled under.
type Claim struct {
	store *Store
	alloc *Alloc
	seat  Seat
	claim paths.PoolSeat

	mu    sync.Mutex
	ended bool
}

// Claim claims a placement into the seat: `<seat>.new-<claim>` made 0700, refusing any
// `<seat>.new-*` already there (ErrLeftover, naming it) and a seat whose held/ exists (ErrHeld:
// release it first).
func (s *Store) Claim(a *Alloc, seat Seat) (*Claim, error) {
	if !s.holds(a) {
		return nil, ErrNotLocked
	}
	if err := s.refuseLeftovers(seat); err != nil {
		return nil, err
	}
	pool := s.store.PoolSeat(s.checkout, seat.Team, seat.Name)
	held, err := s.root.exists(pool.Held())
	if err != nil {
		return nil, err
	}
	if held {
		return nil, fmt.Errorf("%w: %s; release it first", ErrHeld, seat.Name)
	}
	id, err := NewClaim()
	if err != nil {
		return nil, err
	}
	if err := s.root.mkdirAll(s.store.PoolTeam(s.checkout, seat.Team)); err != nil {
		return nil, err
	}
	claim := s.store.PoolClaim(s.checkout, seat.Team, seat.Name, id)
	if err := s.root.mkdirFresh(claim.Dir()); err != nil {
		return nil, err
	}
	return &Claim{store: s, alloc: a, seat: seat, claim: claim}, nil
}

// Abandon removes the empty claimed directory, after a refusal from root.
func (c *Claim) Abandon() error {
	if err := c.end(); err != nil {
		return err
	}
	return c.store.root.rmdir(c.claim.Dir())
}

// Fill writes the issued credential, then seat.json, into the claim, and swaps it in: by
// RENAME_NOREPLACE when the seat has no record, else by RENAME_EXCHANGE, the old record moved
// to quarantine with its credential unlinked (SP2). An answer that is not this seat's pool
// record, or not this store's credential, is refused before any write; so is one for a team the
// checkout has on another homeserver (X8), and one that would create the seat for a homeserver
// that does not answer (X10). The team's homeserver is recorded once the seat is in place.
func (c *Claim) Fill(ctx context.Context, is Issued) error {
	s := c.store
	if !s.holds(c.alloc) {
		return ErrNotLocked
	}
	data, p, err := c.checkIssued(ctx, is)
	if err != nil {
		return err
	}
	pool := s.store.PoolSeat(s.checkout, c.seat.Team, c.seat.Name)
	released, err := s.lastRelease(pool)
	if err != nil {
		return err
	}
	if err := c.end(); err != nil {
		return err
	}
	if err := s.root.create(s.credentialPath(c.claim), []byte(is.Secret.Reveal())); err != nil {
		return err
	}
	if err := s.root.create(c.claim.Bundle(), data); err != nil {
		return err
	}
	if released != nil {
		if err := s.root.create(c.claim.ReleasedRecord(), released); err != nil {
			return err
		}
	}
	if err := s.swapIn(c.seat, c.claim.Dir()); err != nil {
		return err
	}
	return p.write()
}

// checkIssued is the checks of an issued bundle that come before any write: it is this store's
// kind, and this claim's team and handle; it is the checkout's homeserver for the team (the pin,
// which a later write records); and a seat that is new, with no pool directory yet, has a
// homeserver that answers from this machine. It answers seat.json's text and the pin.
func (c *Claim) checkIssued(ctx context.Context, is Issued) ([]byte, pin, error) {
	s := c.store
	if is.Kind != s.credential.Kind() {
		return nil, pin{}, invalid(fmt.Sprintf("issued bundle: a %s, not this pool's %s", is.Kind, s.credential.Kind()))
	}
	if is.Seat.Team != c.seat.Team {
		return nil, pin{}, invalid("issued bundle: another team's seat")
	}
	data, err := is.Seat.Encode()
	if err != nil {
		return nil, pin{}, err
	}
	if is.Seat.Handle().String() != c.seat.Name.String() {
		return nil, pin{}, invalid("issued bundle: another handle's seat")
	}
	p, err := s.pinOf(is.Seat)
	if err != nil {
		return nil, pin{}, err
	}
	found, err := s.root.exists(s.store.PoolSeat(s.checkout, c.seat.Team, c.seat.Name).Dir())
	if err != nil {
		return nil, pin{}, err
	}
	if !found {
		if err = s.reachNew(ctx, is.Seat); err != nil {
			return nil, pin{}, err
		}
	}
	return data, p, nil
}

// lastRelease is the seat's released.json, checked, for a placement to carry over the record
// it replaces (n2c-D8): released_ms bounds area B's store-less start, which a reissue must not
// reset. Nil when the seat has none.
func (s *Store) lastRelease(pool paths.PoolSeat) ([]byte, error) {
	data, err := s.root.read(pool.ReleasedRecord(), maxRecord)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := decodeReleasedRecord(data); err != nil {
		return nil, fmt.Errorf("%s: %w", pool.ReleasedRecord(), err)
	}
	return data, nil
}

func (c *Claim) end() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ended {
		return errors.New("seat: the claim is already filled or abandoned")
	}
	c.ended = true
	return nil
}

// swapIn renames the filled claim as the seat's pool directory, exchanging it with an old
// record, which then goes to quarantine without its credential.
func (s *Store) swapIn(seat Seat, claimDir string) error {
	pool := s.store.PoolSeat(s.checkout, seat.Team, seat.Name)
	err := s.root.rename(claimDir, pool.Dir(), unix.RENAME_NOREPLACE)
	if !errors.Is(err, unix.EEXIST) {
		return err
	}
	if err := s.root.rename(claimDir, pool.Dir(), unix.RENAME_EXCHANGE); err != nil {
		return err
	}
	return s.quarantine(seat, claimDir, s.credentialName(seat))
}

// QuarantineLeftovers moves every `<seat>.new-*` of the seat to quarantine, its credential
// unlinked: the reissue's first step (SP1).
func (s *Store) QuarantineLeftovers(a *Alloc, seat Seat) error {
	if !s.holds(a) {
		return ErrNotLocked
	}
	names, err := s.leftovers(seat)
	if err != nil {
		return err
	}
	team := s.store.PoolTeam(s.checkout, seat.Team)
	for _, name := range names {
		if err := s.quarantine(seat, team+"/"+name, s.credentialName(seat)); err != nil {
			return err
		}
	}
	return nil
}
