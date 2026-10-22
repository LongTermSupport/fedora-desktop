package seat

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

// claimMark is in the name of a placement's claim directory beside the pool seat it fills.
const claimMark = ".new-"

// Holdings lists every pool seat of the checkout with a held/, by team and seat name. The pool
// is the owner's alone, so a name in it the store never makes is ErrInvalid, not skipped.
func (s *Store) Holdings() ([]Holding, error) {
	checkout := s.store.PoolCheckout(s.checkout)
	teams, err := s.root.entries(checkout)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Holding
	for _, name := range teams {
		if name == filepath.Base(s.store.PoolCheckoutRecord(s.checkout)) {
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

// teamHoldings is Holdings for one team's directory; a placement's claim directory is skipped.
func (s *Store) teamHoldings(team paths.TeamID) ([]Holding, error) {
	dir := s.store.PoolTeam(s.checkout, team)
	names, err := s.root.entries(dir)
	if err != nil {
		return nil, err
	}
	var out []Holding
	for _, name := range names {
		if strings.Contains(name, claimMark) {
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
