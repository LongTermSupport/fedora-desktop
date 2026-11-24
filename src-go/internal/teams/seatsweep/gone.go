package seatsweep

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"net/url"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// GoneTeams is seat.Teams from this machine's own records (issue 386): what the seat store's
// release asks once a seat's logout got no answer at all. A team is gone only when this machine
// served it and no local source knows it now. The sources, every one read on each question:
//
//   - the seat's own bundle: its base_url must be an IP literal that is an address of this
//     machine (Local), or the team was never this machine's to judge;
//   - the status root (paths.StatusRoot, StatusRoot here) must exist: this machine hosts teams;
//   - the team's status record (paths.StatusFile, read by Status): a team installed here has one
//     from its `team apply` until its `team remove`;
//   - every session's desired.json under the seat store's control/ (paths.Store.DesiredFile): a
//     team any session desires is known, whatever declares it (an ad-hoc team included); a
//     session with no desired.json yet is a launch still taking its seats, so nothing is gone.
//
// Anything else answers not gone; a source that cannot be read is an error. Either way the
// release keeps today's behaviour and is retried, so a live token is never left valid by it.
type GoneTeams struct {
	// Store is the owner's seat store, whose control/ holds each session's desired.json.
	Store paths.Store
	// StatusRoot is the directory the status records are under (paths.StatusRoot).
	StatusRoot string
	// Status reads a team's status record (system.StatusRecord).
	Status func(paths.TeamID) (status.Record, bool, error)
	// Local is whether an address is one of this machine's (LocalAddress).
	Local func(netip.Addr) (bool, error)
}

// MachineTeams is GoneTeams on this machine: the owner's seat store found from getenv's HOME and
// XDG_STATE_HOME as the sweep finds it, the real status records and interface addresses. A seat
// store that cannot be found is every question's error.
func MachineTeams(getenv func(string) string, record func(paths.TeamID) (status.Record, bool, error)) seat.Teams {
	home, err := paths.StateHome(getenv("HOME"), getenv("XDG_STATE_HOME"))
	if err != nil {
		return unjudged{fmt.Errorf("the state home: %w", err)}
	}
	store, err := paths.SeatStore(home)
	if err != nil {
		return unjudged{fmt.Errorf("the seat store: %w", err)}
	}
	return GoneTeams{Store: store, StatusRoot: paths.StatusRoot, Status: record, Local: LocalAddress}
}

// unjudged is a seat.Teams that cannot judge: every question is its error.
type unjudged struct{ err error }

func (u unjudged) Gone(seat.Bundle) (string, error) { return "", u.err }

// Gone is why the team of b is gone for good, "" when it is not known to be.
func (g GoneTeams) Gone(b seat.Bundle) (string, error) {
	u, err := url.Parse(b.BaseURL)
	if err != nil {
		return "", fmt.Errorf("the base_url of team %s: %w", b.Team, err)
	}
	addr, literal := ipLiteral(u.Hostname())
	if !literal {
		return "", nil
	}
	local, err := g.Local(addr)
	if err != nil || !local {
		return "", err
	}
	hosts, err := isDir(g.StatusRoot)
	if err != nil || !hosts {
		return "", err
	}
	_, installed, err := g.Status(b.Team)
	if err != nil || installed {
		return "", err
	}
	desired, err := g.desired(b.Team)
	if err != nil || desired {
		return "", err
	}
	return fmt.Sprintf("team %s has no status record on this machine and no session's desired.json lists it, and its homeserver at %s, an address of this machine, did not answer",
		b.Team, u.Host), nil
}

// desired is whether a session's desired.json lists team, or a session has none yet (a launch
// still taking its seats, which may be of team).
func (g GoneTeams) desired(team paths.TeamID) (bool, error) {
	entries, err := os.ReadDir(g.Store.Root() + "/control")
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d := Desired{Store: g.Store}
	for _, e := range entries {
		session, err := paths.ParseSessionName(e.Name())
		if err != nil {
			return false, fmt.Errorf("control/%s: %w", e.Name(), err)
		}
		info, err := os.Lstat(g.Store.DesiredFile(session))
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("the desired.json of session %s is not a regular file", session)
		}
		got, _, err := d.read(session)
		if err != nil {
			return false, fmt.Errorf("the desired.json of session %s: %w", session, err)
		}
		for _, s := range got.Seats {
			if s.Team == team {
				return true, nil
			}
		}
	}
	return false, nil
}

// ipLiteral is host as an IP address, and false for a host name (whose address this machine
// does not look up: a name is no proof the team was served here).
func ipLiteral(host string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(host)
	return a, err == nil
}

// isDir is whether path is a directory; not there is false.
func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// LocalAddress is whether a is an address of this machine: loopback, or one of its interfaces'.
func LocalAddress(a netip.Addr) (bool, error) {
	if a.IsLoopback() {
		return true, nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false, fmt.Errorf("this machine's addresses: %w", err)
	}
	for _, n := range addrs {
		if p, err := netip.ParsePrefix(n.String()); err == nil && p.Addr().Unmap() == a.Unmap() {
			return true, nil
		}
	}
	return false, nil
}
