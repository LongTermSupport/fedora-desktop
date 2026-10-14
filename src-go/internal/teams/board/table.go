// Package board is the table of teams, seats and people ccy-teams shows: one pass over the
// machine (systemd, tmux, the session registry, the declaration, agent-bus) read into a
// Snapshot of rows, and the --list flow that prints it. It owns what a row says and how it is
// painted; what to do about a row is the picker's.
package board

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Terminal colours of the table: green is working, yellow wants a look, red is not working.
const (
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
)

// The kinds of row: a team, a seat or member run by ccy, a person.
const (
	KindTeam  Kind = "team"
	KindCcy   Kind = "ccy"
	KindHuman Kind = "human"
)

// Kind is a row's KIND. A member that is not run by ccy (lxc, docker, vm, host) carries the
// type word it was listed with.
type Kind string

const rowFormat = "%-24s  %-36s  %-6s  %-13s  %-18s  %-26s  %-7s  %s"

// Row is one line of the table. Handle and Dir are what a key pressed on the row acts on and
// are not shown; "-" stands for none, as in the printed table.
type Row struct {
	Team, Member, State, Applied, Session, Restore, Where string
	Kind                                                  Kind
	Handle, Dir                                           string
	// Record is the registry name of the session holding a seat; Session shows it with "(stopped)"
	// when it is not running.
	Record string
	// Role is the member's role in the team (worker, coordinator), empty for a row that is not a
	// member or whose members could not be read.
	Role string
}

// coordinatorMark follows the seat of the team's coordinator in the MEMBER column.
const coordinatorMark = " (coordinator)"

// HeadingText is the table's heading line.
func HeadingText() string {
	return fmt.Sprintf(rowFormat, "TEAM", "MEMBER", "KIND", "STATE", "APPLIED", "SESSION", "RESTORE", "WHERE")
}

// Text is the row as plain text: what --list prints when piped and what the picker hands back.
func (r Row) Text() string {
	return fmt.Sprintf(rowFormat, r.Team, r.memberCell(), r.Kind, r.State, r.Applied, r.Session, r.Restore, r.Where)
}

// Painted is Text with the team row in bold and each status word in its colour; stripped of
// colour codes it is the plain row.
func (r Row) Painted() string {
	team := fmt.Sprintf("%-24s", r.Team)
	if r.Kind == KindTeam {
		team = term.Bold + team + term.Reset
	}
	return fmt.Sprintf("%s  %-36s  %-6s  %s  %s  %s  %s  %s", team, r.memberCell(), r.Kind,
		paint(13, r.State), paint(18, r.Applied), paint(26, r.Session), paint(7, r.Restore), r.Where)
}

// memberCell is the MEMBER column: the seat, marked when it is the team's coordinator. A member
// shown by its handle (no checkout known) is too wide for a mark and says its role in WHERE.
func (r Row) memberCell() string {
	if r.Kind == KindCcy && r.Dir != "-" && r.Role == agentbus.RoleCoordinator {
		return r.Member + coordinatorMark
	}
	return r.Member
}

// paint pads word to width, inside its colour codes so the columns line up.
func paint(width int, word string) string {
	padded := fmt.Sprintf("%-*s", width, word)
	if colour := wordColour(word); colour != "" {
		return colour + padded + term.Reset
	}
	return padded
}

// wordColour is the colour a status word is shown in, or "" for none.
func wordColour(word string) string {
	switch {
	case isOneOf(word, "free", "invited", "no", "parked", "never signed in"), strings.HasSuffix(word, "(stopped)"):
		return cYellow
	case isOneOf(word, "up", "active", "held", "in room", "yes"), strings.HasSuffix(word, " signed in"):
		return cGreen
	case strings.HasPrefix(word, "out of date"), word == declare.WordNotBuilt:
		return cYellow
	case word == declare.WordUpToDate:
		return cGreen
	case strings.HasPrefix(word, "down"), strings.HasPrefix(word, "decide:"),
		isOneOf(word, "not installed", "not in room", "unknown", "need sudo", "unreadable"):
		return cRed
	}
	return ""
}

func isOneOf(word string, options ...string) bool {
	return slices.Contains(options, word)
}
