// Package agentbus runs the agent-bus wrapper and its installer and parses the tab-separated
// marker lines they print (MEMBER, HUMAN, DEVICE, BACKUP). Lines of any other kind
// (CHANGED, notes) are not this package's and are skipped; a known marker with the wrong
// shape is an error, so a changed wrapper fails loudly instead of yielding half a row.
package agentbus

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/marker"
)

// Seat is one seat of a checkout, as "agent-bus seat list" named it (SeatList reads it in-process).
type Seat struct {
	Team   string
	Seat   string
	Held   bool
	Handle string
}

// Human is a HUMAN line: a person of the team room. Membership is "join", "invite" or
// empty when the wrapper printed "-".
type Human struct {
	Name       string
	Membership string
}

// The roles of a team's agents (protocol §8): at most one coordinator, and workers.
const (
	RoleCoordinator = "coordinator"
	RoleWorker      = "worker"

	// legacyCoordinator is what the coordinator was called before it had that name; a
	// wrapper that has not been updated still prints it.
	legacyCoordinator = "orchestrator"
)

// The registry's park words on a MEMBER line. WordNotParked is printed "active" by agent-bus
// list, and means only that the member is not parked: it says nothing of whether it runs.
const (
	WordNotParked = "active"
	WordParked    = "parked"
)

// Liveness is whether a member's session is listening on the bus now: its LIVE line, which
// `agent-bus list` judges from the member's agent_bus.status in the team room as `pingbus status`
// does (a status whose lease runs past now is listening; a lapsed one is stale). It is the one
// liveness source of a member; the MEMBER line's park state says nothing about it.
type Liveness string

const (
	// LiveUnreported is a member with no LIVE line: the agent-bus wrapper is older than LIVE lines.
	LiveUnreported Liveness = ""
	// LiveListening is a member whose status lease runs past now: a session of it is listening.
	LiveListening Liveness = "listening"
	// LiveStale is a member whose last status lease has ended: no session of it is listening.
	LiveStale Liveness = "stale"
	// LiveNoStatus is a member with no status in the room ("-"): no session of it has listened.
	LiveNoStatus Liveness = "-"
)

// Member is a MEMBER line, an agent account of the team, with what its LIVE line said.
type Member struct {
	Handle     string
	Role       string
	Membership string
	// Parked is the registry's park state (MEMBER's "parked"; "active" is only not parked).
	Parked bool
	Live   Liveness
}

// Kind is the member's kind, KindOf its handle.
func (m Member) Kind() string { return KindOf(m.Handle) }

// KindOf is what runs the member a handle (<repo>.<seat>+<host>.<type>) names, from its type
// word: podman is ccy (a ccy container), host is cc (a cc session on the desktop), and lxc,
// docker and vm are themselves. It is the one reading of the type word for a person.
func KindOf(handle string) string {
	kind := handle
	if i := strings.LastIndex(handle, "."); i >= 0 {
		kind = handle[i+1:]
	}
	switch kind {
	case "podman":
		return "ccy"
	case "host":
		return "cc"
	}
	return kind
}

// Roster is what "agent-bus list <team>" printed.
type Roster struct {
	Humans  []Human
	Members []Member
}

// Coordinator is the member holding the coordinator role, parked or not (a parked member keeps
// its role), and whether there is one.
func (r Roster) Coordinator() (Member, bool) {
	for _, m := range r.Members {
		if m.Role == RoleCoordinator {
			return m, true
		}
	}
	return Member{}, false
}

// Member is the member with handle, and whether there is one.
func (r Roster) Member(handle string) (Member, bool) {
	for _, m := range r.Members {
		if m.Handle == handle {
			return m, true
		}
	}
	return Member{}, false
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

// Choice is one CHOICE line of "agent-bus seat choose": a seat the chooser offers, its state
// (free, held or new), the member's handle and the type in words.
type Choice struct {
	Seat, State, Handle, Words string
}

// Choices is what "agent-bus seat choose" printed: the seats offered and the one to start on.
type Choices struct {
	Rows    []Choice
	Default string
}

// ParseChoices reads the CHOICE and DEFAULT lines of out. Nothing else may be in it, there must
// be a row to choose, and the default must be one of them: a wrapper that answers otherwise is
// out of date, and that fails loudly.
func ParseChoices(out string) (Choices, error) {
	var c Choices
	for line := range strings.Lines(out) {
		text := strings.TrimRight(line, "\r\n")
		if text == "" {
			continue
		}
		f := strings.Split(text, "\t")
		switch {
		case f[0] == "CHOICE" && len(f) == 5:
			c.Rows = append(c.Rows, Choice{Seat: f[1], State: f[2], Handle: f[3], Words: f[4]})
		case f[0] == "DEFAULT" && len(f) == 2:
			c.Default = f[1]
		default:
			return Choices{}, fmt.Errorf("agent-bus seat choose printed an unknown line %q", text)
		}
	}
	if !slices.ContainsFunc(c.Rows, func(r Choice) bool { return r.Seat == c.Default }) {
		return Choices{}, errors.New("agent-bus seat choose gave no usable rows")
	}
	return c, nil
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
		if m.Role == legacyCoordinator {
			m.Role = RoleCoordinator
		}
		switch f[4] {
		case WordParked:
			m.Parked = true
		case WordNotParked:
		default:
			return r, fmt.Errorf("MEMBER %s: activity %q is neither active nor parked", f[1], f[4])
		}
		r.Members = append(r.Members, m)
	}
	return r, readLiveness(out, r.Members)
}

// readLiveness sets each member's Live from the LIVE lines of out: one at most per member, for a
// member a MEMBER line named, in a word it knows.
func readLiveness(out string, members []Member) error {
	lines, err := marker.Fields(out, "LIVE", 3)
	if err != nil {
		return err
	}
	for _, f := range lines {
		i := slices.IndexFunc(members, func(m Member) bool { return m.Handle == f[1] })
		switch {
		case i < 0:
			return fmt.Errorf("LIVE %s: no MEMBER line names it", f[1])
		case members[i].Live != LiveUnreported:
			return fmt.Errorf("LIVE %s: said twice", f[1])
		}
		switch live := Liveness(f[2]); live {
		case LiveListening, LiveStale, LiveNoStatus:
			members[i].Live = live
		case LiveUnreported:
			return fmt.Errorf("LIVE %s: the liveness is empty", f[1])
		default:
			return fmt.Errorf("LIVE %s: %q is neither listening, stale nor -", f[1], f[2])
		}
	}
	return nil
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
