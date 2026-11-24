package seatsweep

import (
	"net/netip"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// GoneTeams is seat.Teams from this machine's own records.
type GoneTeams struct {
	Store      paths.Store
	StatusRoot string
	Status     func(paths.TeamID) (status.Record, bool, error)
	Local      func(netip.Addr) (bool, error)
}

// Gone is not yet judged.
func (g GoneTeams) Gone(seat.Bundle) (string, error) { return "", nil }

// LocalAddress is not yet judged.
func LocalAddress(netip.Addr) (bool, error) { return false, nil }
