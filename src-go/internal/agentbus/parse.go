// Package agentbus runs the agent-bus wrapper and its installer and parses the tab-separated
// marker lines they print (SEAT, MEMBER, HUMAN, DEVICE, BACKUP). Lines of any other kind
// (CHANGED, notes) are not this package's and are skipped; a known marker with the wrong
// shape is an error, so a changed wrapper fails loudly instead of yielding half a row.
package agentbus

import (
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/marker"
)

// Seat is one SEAT line of "agent-bus seat list".
type Seat struct {
	Team   string
	Seat   string
	Held   bool
	Self   bool // held by the checkout the command ran in
	Handle string
}

// Human is a HUMAN line: a person of the team room. Membership is "join", "invite" or
// empty when the wrapper printed "-".
type Human struct {
	Name       string
	Membership string
}

// Member is a MEMBER line: an agent account of the team.
type Member struct {
	Handle     string
	Role       string
	Membership string
	Parked     bool
}

// Kind is the handle's type word (podman, lxc, docker, vm, host), with podman shown as ccy.
func (m Member) Kind() string {
	kind := m.Handle
	if i := strings.LastIndex(m.Handle, "."); i >= 0 {
		kind = m.Handle[i+1:]
	}
	if kind == "podman" {
		return "ccy"
	}
	return kind
}

// Roster is what "agent-bus list <team>" printed.
type Roster struct {
	Humans  []Human
	Members []Member
}

// Device is a DEVICE line of "agent-bus human devices": one signed-in Element session.
type Device struct {
	ID         string
	LastSeenMs string // empty when the wrapper printed "-"
}

// Backup is a BACKUP line of "agent-bus-install backups".
type Backup struct {
	ID   string
	Time string
}

// ParseSeats reads the SEAT lines of out.
func ParseSeats(out string) ([]Seat, error) {
	rows, err := marker.Fields(out, "SEAT", 6)
	if err != nil {
		return nil, err
	}
	seats := make([]Seat, 0, len(rows))
	for _, f := range rows {
		s := Seat{Team: f[1], Seat: f[2], Handle: f[5]}
		switch f[3] {
		case "held":
			s.Held = true
		case "free":
		default:
			return nil, fmt.Errorf("SEAT %s@%s: state %q is neither held nor free", f[2], f[1], f[3])
		}
		switch f[4] {
		case "self":
			s.Self = true
		case "-":
		default:
			return nil, fmt.Errorf("SEAT %s@%s: owner %q is neither self nor -", f[2], f[1], f[4])
		}
		seats = append(seats, s)
	}
	return seats, nil
}

// ParseRoster reads the HUMAN and MEMBER lines of out.
func ParseRoster(out string) (Roster, error) {
	var r Roster
	humans, err := marker.Fields(out, "HUMAN", 3)
	if err != nil {
		return r, err
	}
	for _, f := range humans {
		r.Humans = append(r.Humans, Human{Name: f[1], Membership: marker.DashEmpty(f[2])})
	}
	members, err := marker.Fields(out, "MEMBER", 5)
	if err != nil {
		return r, err
	}
	for _, f := range members {
		m := Member{Handle: f[1], Role: f[2], Membership: marker.DashEmpty(f[3])}
		switch f[4] {
		case "parked":
			m.Parked = true
		case "active":
		default:
			return r, fmt.Errorf("MEMBER %s: activity %q is neither active nor parked", f[1], f[4])
		}
		r.Members = append(r.Members, m)
	}
	return r, nil
}

// ParseDevices reads the DEVICE lines of out.
func ParseDevices(out string) ([]Device, error) {
	rows, err := marker.Fields(out, "DEVICE", 3)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(rows))
	for _, f := range rows {
		devices = append(devices, Device{ID: f[1], LastSeenMs: marker.DashEmpty(f[2])})
	}
	return devices, nil
}

// ParseBackups reads the BACKUP lines of out.
func ParseBackups(out string) ([]Backup, error) {
	rows, err := marker.Fields(out, "BACKUP", 3)
	if err != nil {
		return nil, err
	}
	backups := make([]Backup, 0, len(rows))
	for _, f := range rows {
		backups = append(backups, Backup{ID: f[1], Time: f[2]})
	}
	return backups, nil
}
