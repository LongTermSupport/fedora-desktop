// Package coordinator is how every place that can make a member a team's coordinator words the
// two paths (Plan 00203 SPEC-hot-control.md H25): a team with no coordinator is offered one
// plainly, and a team that has one is a different, rarer proposition, a takeover, which names
// the current coordinator, says whether it is running, and that it loses the role. hot (the
// session commands), seats (the picker's seat menu and the launcher's chooser), choose (the
// launch role question) and the picker's session view all take their words from here.
package coordinator

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"

// Running is what is known here of a coordinator's session.
type Running int

const (
	// Unknown is a member whose seat nothing here can read (another checkout, an LXC member).
	Unknown Running = iota
	// Active is a member whose seat a live process holds: its session runs.
	Active
	// Stopped is a member whose seat no process holds.
	Stopped
	// Parked is a parked member: it keeps its role but takes no part.
	Parked
)

// Lookup says what is known of a member's session.
type Lookup func(agentbus.Member) Running

// Holder is the team's current coordinator as a question names it.
type Holder struct {
	Handle  string
	Running Running
}

// Named is the holder with whether it is running: "<handle> (running)".
func (h Holder) Named() string {
	words := "whether it is running is not known here"
	switch h.Running {
	case Active:
		words = "running"
	case Stopped:
		words = "not running"
	case Parked:
		words = "parked"
	case Unknown:
	}
	return h.Handle + " (" + words + ")"
}

// Of is the member other than handle that holds the team's coordinator role, and whether there
// is one.
func Of(roster agentbus.Roster, handle string) (agentbus.Member, bool) {
	m, held := roster.Coordinator()
	if !held || m.Handle == handle {
		return agentbus.Member{}, false
	}
	return m, true
}

// Holding is Of as a Holder, with what look knows of it (nil knows nothing); an empty Handle is
// a team with no other coordinator.
func Holding(roster agentbus.Roster, handle string, look Lookup) Holder {
	m, held := Of(roster, handle)
	if !held {
		return Holder{}
	}
	h := Holder{Handle: m.Handle, Running: Unknown}
	if look != nil {
		h.Running = look(m)
	}
	return h
}

// FromSeats is a Lookup over seats read with agent-bus seat list: a parked member is Parked
// whatever its seat, a seat a process holds is Active, one it does not is Stopped, and a member
// with no seat here is Unknown.
func FromSeats(seats []agentbus.Seat) Lookup {
	return func(m agentbus.Member) Running {
		if m.Parked {
			return Parked
		}
		for _, s := range seats {
			if s.Handle == m.Handle {
				if s.Held {
					return Active
				}
				return Stopped
			}
		}
		return Unknown
	}
}

// OfferNone is the plain path: the team has no coordinator.
func OfferNone(team, who string) string {
	return team + " has no coordinator. Make " + who + " its coordinator?"
}

// OfferTakeover is the rare path: the team has a coordinator, and this removes it.
func OfferTakeover(team, who string, h Holder) string {
	return team + "'s coordinator is " + h.Named() + ". A team has one coordinator. Remove " + h.Handle +
		" as coordinator and make " + who + " the coordinator instead?"
}

// NoneChoice is the role-list item for the plain path.
func NoneChoice(team string) string { return "Make it the coordinator (" + team + " has none)" }

// MakeChoice is the plain path as a member's menu line, naming the member: the picker offers it
// on the row of a member of any kind.
func MakeChoice(who, team string) string {
	return "Make " + who + " the coordinator (" + team + " has none)"
}

// WorkerChoice is the coordinator's own menu line, naming it: it leads to a question that says
// the team then has no coordinator and defaults to no.
func WorkerChoice(who, team string) string {
	return "Make " + who + " a worker (" + team + " then has no coordinator)"
}

// TakeoverChoice is the role-list item for the takeover: never a plain "coordinator", and it
// leads to a question that defaults to no.
func TakeoverChoice(h Holder) string {
	return "Replace the coordinator, " + h.Named() + ": it becomes a worker..."
}

// Refusal is a takeover asked for without its flag, where nothing can be asked.
func Refusal(team, who string, h Holder, flag string) string {
	return team + "'s coordinator is " + h.Named() + ". A team has one coordinator: to remove " + h.Handle +
		" as coordinator and make " + who + " the coordinator instead, add " + flag + ". Nothing was changed."
}
