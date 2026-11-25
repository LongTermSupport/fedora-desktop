// Package seatchoice is what a launch with a teamchat team and no seat named offers of this
// checkout's pool seats of the team (issue 423). v1's `agent-bus seat choose` is the floor: every
// seat is listed with whether a running session holds it, the default is the lowest free seat,
// and a held seat is offered as a re-attach to the session holding it, never as a launch, since
// its take is refused. Where no seat can be taken, the refusal names the seats, which are held,
// and the command for each way on. Pure: the caller reads the pool and the holdings.
//
//	S1  Order: numbered seats first, by number, then named ones by name (v1's _seat_order).
//	S2  Default: the lowest free seat; none when every seat is held.
//	S3  Row: the seat, its handle, and free or the session holding it with the re-attach.
//	S4  WaysOn: every seat with its state, then a free seat's launch, a held seat's re-attach,
//	    and a new seat (AddCommand, `ccy-teams seats add <team>`).
//	S5  WaysOn with no seat names the way to have one.
//	S7  A seat's kind is its handle's type (podman for ccy, host for cc); a launch is offered,
//	    and its ways on name, only the seats of its own kind (OfKind; register R55).
//	S8  WrongKind refuses a seat of the other kind named for a launch: its kind, its launcher,
//	    and the add of a seat of the launch's kind.
package seatchoice

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Seat is one pool seat of the checkout in a team.
type Seat struct {
	// Name is the seat part of the handle, as --teams names it (<seat>@<team>).
	Name   string
	Handle string
	// HeldBy is the session a take would be refused for (live or restorable); "" is free.
	HeldBy string
}

// Held is whether a session holds the seat.
func (s Seat) Held() bool { return s.HeldBy != "" }

// Order is the seats numbered ones first, by number, then named ones by name.
func Order(seats []Seat) []Seat {
	out := slices.Clone(seats)
	slices.SortStableFunc(out, func(a, b Seat) int {
		an, aNum := number(a.Name)
		bn, bNum := number(b.Name)
		switch {
		case aNum && bNum:
			return an - bn
		case aNum:
			return -1
		case bNum:
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func number(name string) (int, bool) {
	n, err := strconv.Atoi(name)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == name
}

// Default is the lowest free seat, and false when there is none.
func Default(seats []Seat) (Seat, bool) {
	for _, s := range Order(seats) {
		if !s.Held() {
			return s, true
		}
	}
	return Seat{}, false
}

// Row is the seat as the chooser shows it.
func Row(s Seat) string {
	state := "free"
	if s.Held() {
		state = "held by session " + s.HeldBy + ": re-attach it"
	}
	return fmt.Sprintf("seat %-6s  %-40s  %s", s.Name, s.Handle, state)
}

// The kinds of pool seat: a handle's type, and the --type a launcher gives (ccy podman, cc host).
const (
	KindPodman = "podman"
	KindHost   = "host"
)

// KindOf is the kind of the seat whose handle is handle, "" for a name that is not a handle.
func KindOf(handle string) string {
	h, ok := proto.ParseHandle(handle)
	if !ok {
		return ""
	}
	return h.Type
}

// OfKind is the seats of kind, in their order: the only pool a launch of kind is offered.
func OfKind(seats []Seat, kind string) []Seat {
	var out []Seat
	for _, s := range seats {
		if KindOf(s.Handle) == kind {
			out = append(out, s)
		}
	}
	return out
}

// Other is the launcher kind that is not kind: a ccy launch's other is host, a cc launch's podman.
func Other(kind string) string {
	if kind == KindHost {
		return KindPodman
	}
	return KindHost
}

// Launcher is the command that launches a seat of kind: cc for host, ccy for podman.
func Launcher(kind string) string {
	if kind == KindHost {
		return "cc"
	}
	return "ccy"
}

// WrongKind is the refusal of seat@team, whose handle is of another kind, for a launch of kind:
// the seat's kind, the launcher it belongs to, and the add of a seat of the launch's own kind.
func WrongKind(seat, team, handle, kind string) string {
	ref := seat + "@" + team
	other := KindOf(handle)
	return ref + " is a " + other + " seat (" + handle + "), and this is a " + Launcher(kind) + " (" + kind +
		") launch: launch it with " + Launcher(other) + " --teams " + ref + ", or add a " + kind + " seat with " +
		AddCommand(team, kind) + ", then " + Launcher(kind) + " --teams <seat>@" + team
}

// WaysOn are the lines a refusal prints for team in checkout (as shown to a person): the seats
// of kind and their state, then the command for each way on, through the launcher of kind.
func WaysOn(kind, team, checkout string, seats []Seat) []string {
	launcher := Launcher(kind)
	ordered := Order(seats)
	var lines []string
	if len(ordered) == 0 {
		lines = append(lines, "  This checkout ("+checkout+") has no "+kind+" seat of "+team+".")
	} else {
		lines = append(lines, "  Seats of "+team+" in this checkout ("+checkout+"):")
		for _, s := range ordered {
			lines = append(lines, "    "+Row(s))
		}
	}
	lines = append(lines, "  Ways on:")
	for _, s := range ordered {
		if s.Held() {
			lines = append(lines, "    re-attach session "+s.HeldBy+" (seat "+s.Name+"): ccy-sessions, then choose "+s.HeldBy+
				" (attach it, or take it over from another terminal); not running, ccy-sessions restore "+s.HeldBy)
			continue
		}
		lines = append(lines, "    take free seat "+s.Name+": "+launcher+" --teams "+s.Name+"@"+team)
	}
	return append(lines, "    a new seat: "+AddCommand(team, kind)+", then "+launcher+" --teams <seat>@"+team)
}

// AddCommand is the command that adds a new seat of kind of team to this checkout (`ccy-teams
// seats add`, issue 416): the way on when no seat is free.
func AddCommand(team, kind string) string {
	if kind == KindHost {
		return "ccy-teams seats add " + team + " --host"
	}
	return "ccy-teams seats add " + team
}

// NewRow is the chooser's row that asks for a new seat of kind of team.
func NewRow(team, kind string) string {
	return "new " + kind + " seat in " + team + " (" + AddCommand(team, kind) + ")"
}
