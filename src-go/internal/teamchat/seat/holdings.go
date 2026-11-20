package seat

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Holding is a pool seat of the checkout with a held/ (SW1): its held.json, or Complete false
// when that is missing or does not decode, which a release sends to quarantine.
type Holding struct {
	Seat     Seat
	Record   HeldRecord
	Complete bool
}

// Holdings lists every pool seat of the checkout with a held/, by team and seat name. The pool
// is the owner's alone, so a name in it the store never makes is ErrInvalid, not skipped; the
// checkout record and the fresh copy its write leaves after a crash are names the store makes.
func (s *Store) Holdings() ([]Holding, error) {
	checkout := s.store.PoolCheckout(s.checkout)
	teams, err := s.root.entries(checkout)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record := filepath.Base(s.store.PoolCheckoutRecord(s.checkout))
	var out []Holding
	for _, name := range teams {
		if name == record || name == record+replaceSuffix {
			continue
		}
		team, err := paths.ParseTeamID(name)
		if err != nil {
			return nil, fmt.Errorf("%w %s/%s: %w", ErrInvalid, checkout, name, err)
		}
		got, err := s.teamHoldings(team)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// ReadHoldings is Holdings of one checkout read without a store opened for allocation (ccy-teams
// shutdown asks it): no part beyond where the store is, and nothing made; a seat store that does
// not exist holds nothing.
func ReadHoldings(store paths.Store, checkout paths.CheckoutKey) ([]Holding, error) {
	var holdings []Holding
	err := readOnly(store, checkout, func(s *Store) error {
		var err error
		holdings, err = s.Holdings()
		return err
	})
	return holdings, err
}

// Keeper is what a session's seat lock of a team says of its keeper (the L3 probe).
type Keeper int

const (
	// KeeperNoLock is no seat lock: no keeper has made it. A v2 launch writes the holding before
	// its keeper makes the lock, so a session still starting is in this state too.
	KeeperNoLock Keeper = iota
	// KeeperEnded is a seat lock there and free: the run that held it has ended.
	KeeperEnded
	// KeeperRunning is a seat lock held: the session's keeper runs now.
	KeeperRunning
	// KeeperRefused is a seat lock the probe would not open (n2c-D14): nothing is known of its
	// keeper.
	KeeperRefused
)

// KeeperOf is what the session's seat lock of the team says of its keeper, read without a store
// opened for allocation, as ReadHoldings reads; the store's live-keeper test (keeperLive, which a
// take's sessionHolds asks first) is KeeperRunning. ccy-teams shutdown asks it, so a holding a
// run left never makes a later session under the same name the holder. A seat store that does
// not exist has no lock, and nothing is made.
func KeeperOf(store paths.Store, session paths.SessionName, team paths.TeamID) (Keeper, error) {
	got := KeeperNoLock
	err := readOnly(store, paths.CheckoutKey{}, func(s *Store) error {
		var err error
		got, err = s.keeper(session, team)
		return err
	})
	return got, err
}

// keeper is KeeperOf on an open store.
func (s *Store) keeper(session paths.SessionName, team paths.TeamID) (Keeper, error) {
	got, err := s.probe(s.store.SessionSeat(session, team))
	if err != nil {
		return KeeperNoLock, err
	}
	switch got.state {
	case lockHeld:
		return KeeperRunning, nil
	case lockFree:
		return KeeperEnded, nil
	case lockRefused:
		return KeeperRefused, nil
	case lockMissing:
	}
	return KeeperNoLock, nil
}

// keeperLive is whether the session's seat lock of the team is held: its keeper runs now. A lock
// that is missing, free or refused (n2c-D14) has no live keeper.
func (s *Store) keeperLive(session paths.SessionName, team paths.TeamID) (bool, error) {
	got, err := s.keeper(session, team)
	return err == nil && got == KeeperRunning, err
}

// readOnly runs f on the seat store opened with no part made beyond where it is, for one
// checkout; a seat store that does not exist runs nothing. The Store f is given has no sessions,
// and a zero checkout when the caller reads no pool (KeeperOf): only what reads neither may run
// on it, as probe does.
func readOnly(store paths.Store, checkout paths.CheckoutKey, f func(*Store) error) error {
	root, err := openDir(store.Root(), false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(f(&Store{store: store, checkout: checkout, root: root}), root.close())
}

// HeldBy is the holdings the session holds: complete, their record naming it. It is the one
// answer to "which seats does this session hold", for the launcher's exit (Verbs.end) and
// ccy-teams shutdown alike; an incomplete holding names no session, so it is no one's.
func HeldBy(holdings []Holding, session paths.SessionName) []Holding {
	var out []Holding
	for _, h := range holdings {
		if h.Complete && h.Record.Session == session {
			out = append(out, h)
		}
	}
	return out
}

// teamHoldings is Holdings for one team's directory; a placement's claim directory is skipped.
func (s *Store) teamHoldings(team paths.TeamID) ([]Holding, error) {
	dir := s.store.PoolTeam(s.checkout, team)
	names, err := s.root.entries(dir)
	if err != nil {
		return nil, err
	}
	var out []Holding
	for _, name := range names {
		if paths.IsClaimName(name) {
			continue
		}
		seatName, err := paths.ParseSeatName(name)
		if err != nil {
			return nil, fmt.Errorf("%w %s/%s: %w", ErrInvalid, dir, name, err)
		}
		h, rec, complete, err := s.openHeld(s.store.PoolSeat(s.checkout, team, seatName))
		if err != nil {
			return nil, err
		}
		if h == nil {
			continue
		}
		if err := h.close(); err != nil {
			return nil, err
		}
		out = append(out, Holding{Seat: Seat{Team: team, Name: seatName}, Record: rec, Complete: complete})
	}
	return out, nil
}
