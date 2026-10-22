package seat

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

// Claim is a claimed `<seat>.new-<claim>` directory a placement fills (C 3.7.3 "How it is
// placed", SP1, SP2), made under the allocation lock it is filled under.
type Claim struct {
	store *Store
	alloc *Alloc
	seat  Seat
	dir   string

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
	id, err := newClaim()
	if err != nil {
		return nil, err
	}
	if err := s.root.mkdirAll(s.store.PoolTeam(s.checkout, seat.Team)); err != nil {
		return nil, err
	}
	dir := s.store.PoolClaim(s.checkout, seat.Team, seat.Name, id).Dir()
	if err := s.root.mkdirFresh(dir); err != nil {
		return nil, err
	}
	return &Claim{store: s, alloc: a, seat: seat, dir: dir}, nil
}

// Abandon removes the empty claimed directory, after a refusal from root.
func (c *Claim) Abandon() error {
	if err := c.end(); err != nil {
		return err
	}
	return c.store.root.rmdir(c.dir)
}

// Fill writes the issued credential, then seat.json, into the claim, and swaps it in: by
// RENAME_NOREPLACE when the seat has no record, else by RENAME_EXCHANGE, the old record moved
// to quarantine with its credential unlinked (SP2). An answer that is not this seat's pool
// record, or not this store's credential, is refused before any write.
func (c *Claim) Fill(is Issued) error {
	s := c.store
	if !s.holds(c.alloc) {
		return ErrNotLocked
	}
	if is.Kind != s.credential.Kind() {
		return invalid(fmt.Sprintf("issued bundle: a %s, not this pool's %s", is.Kind, s.credential.Kind()))
	}
	if is.Seat.Team != c.seat.Team {
		return invalid("issued bundle: another team's seat")
	}
	data, err := is.Seat.Encode()
	if err != nil {
		return err
	}
	if is.Seat.Handle().String() != c.seat.Name.String() {
		return invalid("issued bundle: another handle's seat")
	}
	if err := c.end(); err != nil {
		return err
	}
	if err := s.root.create(c.dir+"/"+string(is.Kind), []byte(is.Secret.Reveal())); err != nil {
		return err
	}
	if err := s.root.create(c.dir+"/seat.json", data); err != nil {
		return err
	}
	return s.swapIn(c.seat, c.dir)
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
	return s.quarantine(seat, claimDir, s.credentialName())
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
		if err := s.quarantine(seat, team+"/"+name, s.credentialName()); err != nil {
			return err
		}
	}
	return nil
}
