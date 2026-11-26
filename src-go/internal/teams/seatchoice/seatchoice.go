// Package seatchoice is what a launch with a teamchat team and no seat named offers of this
// checkout's pool seats of the team (issue 423). v1's `agent-bus seat choose` is the floor: every
// seat is listed with whether a running session holds it, the default is the lowest free seat,
// and a held seat is offered as a re-attach to the session holding it, never as a launch, since
// its take is refused. Where no seat can be taken, the refusal names the seats, which are held,
// and the command for each way on. Pure: the caller reads the pool and the holdings.
//
//	S1  Order: numbered seats first, by number, then named ones by name (v1's _seat_order).
//	S2  Default: the free seat that last ran here (the newest), else the lowest free seat; none
//	    when every seat is held.
//	S3  Row: the seat in words (Plan 00265 R90), never its name or handle: its role in the team
//	    (Describe), when it last ran here and its conversation's first prompt (Q5), free with
//	    nothing to resume, or the session holding it with the re-attach.
//	S4  WaysOn: every seat in words, then the plain launch by team (which finds or asks for the
//	    seat), a held seat's re-attach, and a new seat (AddCommand, `ccy-teams seats add <team>`).
//	S9  Pick: the seat a launch takes without asking: the one free seat with a conversation
//	    here, or a free one when none ever ran and none is held; anything else is asked.
//	S5  WaysOn with no seat names the way to have one.
//	S7  A seat's kind is its handle's type (podman for ccy, host for cc); a launch is offered,
//	    and its ways on name, only the seats of its own kind (OfKind; register R55).
//	S8  WrongKind refuses a seat of another kind named for a launch: its kind, its launcher
//	    (or that neither launcher takes it), and the add of a seat of the launch's kind.
package seatchoice

import (
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
	// LastRanMS is when the seat's conversation last ran in this checkout (its seat record, while
	// the transcript is here), Unix milliseconds; 0 is a seat with no conversation to resume.
	LastRanMS int64
	// Role is the seat's role in the team, as the team's status record names its member
	// (coordinator, worker); "" where none is known.
	Role string
	// Prompt is the first prompt of the conversation it last ran, cut to one line; "" for none.
	Prompt string
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

// Used is whether the seat has a conversation here to resume.
func (s Seat) Used() bool { return s.LastRanMS > 0 }

// Default is the free seat that last ran here (the newest), else the lowest free seat, and false
// when there is none.
func Default(seats []Seat) (Seat, bool) {
	var best Seat
	found := false
	for _, s := range Order(seats) {
		if s.Held() {
			continue
		}
		if !found || s.LastRanMS > best.LastRanMS {
			best, found = s, true
		}
	}
	return best, found
}

// Pick is the seat a launch takes without asking, and false for a genuine choice: the one free
// seat with a conversation here, or, when no seat ever ran and none is held, the lowest free one
// (they are the same). Two free seats with a conversation, a held seat beside free ones, or no
// free seat, are asked.
func Pick(seats []Seat) (Seat, bool) {
	var free, used []Seat
	for _, s := range Order(seats) {
		switch {
		case s.Held():
			return Seat{}, false
		case s.Used():
			used = append(used, s)
		}
		free = append(free, s)
	}
	switch {
	case len(used) == 1:
		return used[0], true
	case len(used) == 0 && len(free) > 0:
		return free[0], true
	}
	return Seat{}, false
}

// Row is the seat as the chooser shows it, in words: its role, when it last ran here (nowMS is
// the time now) and its first prompt, free with nothing to resume, or the session holding it with
// the re-attach. It never names the seat or its handle (Plan 00265 R90): the chooser maps the row
// back to the seat.
func Row(s Seat, team string, nowMS int64) string {
	switch {
	case s.Held() && s.Role != "":
		return "Running now in session " + s.HeldBy + " (" + InWords(s, team) + "): re-attach it"
	case s.Held():
		return "Running now in session " + s.HeldBy + ": re-attach it"
	case s.Used() && s.Prompt != "":
		return Describe(s, team) + ": last ran " + Ago(nowMS, s.LastRanMS) + `, "` + s.Prompt + `"`
	case s.Used():
		return Describe(s, team) + ": last ran " + Ago(nowMS, s.LastRanMS)
	case s.Role != "" && s.Role != roleCoordinator:
		return "A free " + s.Role + " seat of " + team
	case s.Role == roleCoordinator:
		return "The coordinator of " + team + ": free, with nothing to resume"
	}
	return "A free seat of " + team
}

// roleCoordinator is the role a team has one of.
const roleCoordinator = "coordinator"

// Describe is the seat in words by its role in team (Plan 00265 R90): "The coordinator of
// <team>", "A worker of <team>", or "A seat of <team>" with no role known.
func Describe(s Seat, team string) string {
	switch s.Role {
	case "":
		return "A seat of " + team
	case roleCoordinator:
		return "The coordinator of " + team
	}
	return "A " + s.Role + " of " + team
}

// InWords is Describe inside a sentence: "the coordinator of <team>".
func InWords(s Seat, team string) string { return lowerFirst(Describe(s, team)) }

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// Ago is how long before nowMS thenMS was, in words.
func Ago(nowMS, thenMS int64) string {
	minutes := (nowMS - thenMS) / 60_000
	hours := minutes / 60
	days := hours / 24
	switch {
	case minutes < 1:
		return "just now"
	case hours < 1:
		return plural(minutes, "minute") + " ago"
	case days < 1:
		return plural(hours, "hour") + " ago"
	case days == 1:
		return "yesterday"
	}
	return plural(days, "day") + " ago"
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.FormatInt(n, 10) + " " + unit + "s"
}

// The kinds of pool seat: a handle's type, and the --type a launcher gives (ccy podman, cc host).
// AnyKind is not a kind: asked for, it is every seat with a handle (no handle's type has a "*").
const (
	KindPodman = "podman"
	KindHost   = "host"
	AnyKind    = "*"
)

// IsKind is whether handle is a seat of kind (any handle, for AnyKind).
func IsKind(handle, kind string) bool {
	of := KindOf(handle)
	return of != "" && (kind == AnyKind || of == kind)
}

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
		if IsKind(s.Handle, kind) {
			out = append(out, s)
		}
	}
	return out
}

// Launcher is the command that launches a seat of kind: cc for host, ccy for podman.
func Launcher(kind string) string {
	if kind == KindHost {
		return "cc"
	}
	return "ccy"
}

// WrongKind is the refusal of seat@team, whose handle is of another kind, for a launch of kind:
// the seat's kind, the launcher it belongs to (or that neither launches it, for a kind such as an
// LXC member's), and the add of a seat of the launch's own kind.
func WrongKind(seat, team, handle, kind string) string {
	ref := seat + "@" + team
	other := KindOf(handle)
	way := "launch it with " + Launcher(other) + " --teams " + team + ", or "
	if other != KindPodman && other != KindHost {
		way = "neither ccy nor cc launches it; "
	}
	return ref + " is a " + other + " seat (" + handle + "), and this is a " + Launcher(kind) + " (" + kind +
		") launch: " + way + "add a " + kind + " seat with " + AddCommand(team, kind) + ", then " +
		Launcher(kind) + " --teams " + team
}

// WaysOn are the lines a refusal prints for team in checkout (as shown to a person): the seats
// of kind in words (Row, at nowMS), then the command for each way on, through the launcher of
// kind and by team: the launch finds the seat, and asks when there is a choice.
func WaysOn(kind, team, checkout string, seats []Seat, nowMS int64) []string {
	launcher := Launcher(kind)
	ordered := Order(seats)
	var lines []string
	if len(ordered) == 0 {
		lines = append(lines, "  This checkout ("+checkout+") has no "+kind+" seat of "+team+".")
	} else {
		lines = append(lines, "  Seats of "+team+" in this checkout ("+checkout+"):")
		for _, s := range ordered {
			lines = append(lines, "    "+Row(s, team, nowMS))
		}
	}
	lines = append(lines, "  Ways on:")
	anyFree := false
	for _, s := range ordered {
		if !s.Held() {
			anyFree = true
			continue
		}
		lines = append(lines, "    re-attach session "+s.HeldBy+": ccy-sessions, then choose "+s.HeldBy+
			" (attach it, or take it over from another terminal); not running, ccy-sessions restore "+s.HeldBy)
	}
	if anyFree {
		lines = append(lines, "    take a free seat: run "+launcher+" --teams "+team+" at a terminal, which asks which when there is a choice")
	}
	return append(lines, "    a new seat: "+AddCommand(team, kind)+", then "+launcher+" --teams "+team)
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
