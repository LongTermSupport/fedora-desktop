package host

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// Reach is what a probe through a team's TLS terminator needs (SPEC-cross-network X32): the
// team's CA certificate, and each listen address on the team's port.
type Reach struct {
	CAPEM []byte
	Addrs []netip.AddrPort
}

// AppliedReach is the reach the last apply under root recorded: the listen set of units.json on
// the port of team.json, which is what the terminator was started on, and the CA certificate
// apply made.
func AppliedReach(root string) func(paths.TeamID) (Reach, error) {
	return func(team paths.TeamID) (Reach, error) {
		declared, listen, err := appliedListen(root, team)
		if err != nil {
			return Reach{}, err
		}
		if declared.Port < 1 || declared.Port > 65535 {
			return Reach{}, fmt.Errorf("team.json: %d is not a port", declared.Port)
		}
		caFile := root + paths.CACertFile(team)
		caPEM, found, err := state.ReadFile(caFile, fileMax)
		if err == nil && !found {
			err = errors.New("it is not there: team apply makes it")
		}
		if err != nil {
			return Reach{}, fmt.Errorf("the CA certificate %s: %w", caFile, err)
		}
		addrs := make([]netip.AddrPort, 0, len(listen))
		for _, a := range listen {
			addrs = append(addrs, netip.AddrPortFrom(a, uint16(declared.Port)))
		}
		return Reach{CAPEM: caPEM, Addrs: addrs}, nil
	}
}
