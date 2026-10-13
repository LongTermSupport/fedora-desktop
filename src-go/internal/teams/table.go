package teams

import (
	"fmt"
	"slices"
	"strings"
)

// Terminal colours of the table: green is working, yellow wants a look, red is not working.
const (
	cReset  = "\x1b[0m"
	cBold   = "\x1b[1m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
)

const rowFormat = "%-24s  %-36s  %-6s  %-13s  %-18s  %-26s  %-7s  %s"

// Row is one line of the table. Handle and Dir are what a key pressed on the row acts on and
// are not shown; "-" stands for none, as in the printed table.
type Row struct {
	Team, Member, Kind, State, Applied, Session, Restore, Where string
	Handle, Dir                                                 string
}

// headingText is the table's heading line.
func headingText() string {
	return fmt.Sprintf(rowFormat, "TEAM", "MEMBER", "KIND", "STATE", "APPLIED", "SESSION", "RESTORE", "WHERE")
}

// Text is the row as plain text: what --list prints when piped and what the picker hands back.
func (r Row) Text() string {
	return fmt.Sprintf(rowFormat, r.Team, r.Member, r.Kind, r.State, r.Applied, r.Session, r.Restore, r.Where)
}

// Painted is Text with the team row in bold and each status word in its colour; stripped of
// colour codes it is the plain row.
func (r Row) Painted() string {
	team := fmt.Sprintf("%-24s", r.Team)
	if r.Kind == "team" {
		team = cBold + team + cReset
	}
	return fmt.Sprintf("%s  %-36s  %-6s  %s  %s  %s  %s  %s", team, r.Member, r.Kind,
		paint(13, r.State), paint(18, r.Applied), paint(26, r.Session), paint(7, r.Restore), r.Where)
}

// paint pads word to width, inside its colour codes so the columns line up.
func paint(width int, word string) string {
	padded := fmt.Sprintf("%-*s", width, word)
	if colour := wordColour(word); colour != "" {
		return colour + padded + cReset
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
	case strings.HasPrefix(word, "out of date"), word == "not built":
		return cYellow
	case word == "up to date":
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
