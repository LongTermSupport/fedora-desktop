// Package board is the table of teams, seats and people ccy-teams shows: one pass over the
// machine (systemd, tmux, the session registry, the declaration, agent-bus) read into a
// Snapshot of rows, and the --list flow that prints it. It owns what a row says and how it is
// painted; what to do about a row is the picker's.
package board

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
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

// Row is one line of the table. Member is the seat (a ccy seat with a checkout), the handle (a
// member with none) or the person, as keys and flows name it; the MEMBER column shows a member's
// whole handle instead (Label). Handle and Dir are what a key pressed on the row acts on;
// "-" stands for none, as in the printed table.
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
	// Held is a seat that a live process holds, whichever session it is.
	Held bool
}

// The columns, in order; a column's index is its place in headings and in Row.cells.
const (
	colTeam = iota
	colMember
	colKind
	colState
	colApplied
	colSession
	colRestore
	colWhere
	columns
)

// headings are the column headings.
var headings = [columns]string{"TEAM", "MEMBER", "KIND", "STATE", "APPLIED", "SESSION", "RESTORE", "WHERE"}

// narrowColumns stay on a row of a table too wide for the terminal (TEAM, MEMBER, KIND, STATE:
// what the row is); the rest move off it (Table.Rest).
const narrowColumns = colApplied

// gap is between two columns.
const gap = "  "

// aboutHeading names a team's description where a narrow table moves it off the row.
const aboutHeading = "ABOUT"

// Label is the row's member or person as a person is shown it, in the MEMBER column and in every
// menu and message about the row: a member's whole handle with the coordinator's role beside it
// (agentbus.Label), a person's name. A team row's is its description.
func (r Row) Label() string {
	if r.Kind == KindTeam || r.Kind == KindHuman || r.Handle == "" || r.Handle == "-" {
		return r.Member
	}
	return agentbus.Label(r.Handle, r.Role)
}

// Crumb is how a screen's heading names the row's member: a member by its whole handle
// (agentbus.Label, the role left to the heading's context), a person by its name. A seat with no
// handle yet (ccy has not made its member) is "seat <name>".
func (r Row) Crumb() string {
	noHandle := r.Handle == "" || r.Handle == "-"
	switch {
	case r.Kind == KindCcy && noHandle && r.Dir != "-":
		return "seat " + r.Member
	case r.Kind == KindTeam || r.Kind == KindHuman || noHandle:
		return r.Member
	}
	return agentbus.Label(r.Handle, "")
}

// cells are the row's columns as words.
func (r Row) cells() [columns]string {
	return [columns]string{r.Team, r.Label(), string(r.Kind), r.State, r.Applied, r.Session, r.Restore, r.Where}
}

// Table is a set of rows laid out together: each column as wide as its longest value, measured
// in terminal cells, so no row shifts the columns after a long one; nothing is ever cut short. On
// a terminal too narrow for every column, the table is narrow: the rows keep TEAM, MEMBER, KIND
// and STATE whole, and the other columns, with a team row's description, move off the row to
// Rest, which the picker shows in the row's header and --list on the line below it.
type Table struct {
	widths [columns]int
	narrow bool
}

// Layout lays rows out for a terminal width columns wide; 0 is not a terminal (piped), with no
// limit.
func Layout(rows []Row, width int) Table {
	t := Table{widths: measure(rows, true)}
	if width > 0 && t.lineWidth(columns) > width {
		t = Table{widths: measure(rows, false), narrow: true}
	}
	return t
}

// Narrow reports whether the table is too wide for its terminal (Layout).
func (t Table) Narrow() bool { return t.narrow }

// Heading is the table's heading line.
func (t Table) Heading() string {
	return t.line(headings, func(_ int, padded string) string { return padded })
}

// Text is the row as plain text: what --list prints without colour and what the picker hands back.
func (t Table) Text(r Row) string {
	return t.line(t.shownCells(r), func(_ int, padded string) string { return padded })
}

// Painted is Text with the team row in bold and each status word in its colour; stripped of
// colour codes it is Text.
func (t Table) Painted(r Row) string {
	cells := t.shownCells(r)
	return t.line(cells, func(i int, padded string) string {
		switch {
		case i == colTeam && r.Kind == KindTeam:
			return term.Bold + padded + term.Reset
		case i >= colState:
			if colour := wordColour(cells[i]); colour != "" {
				return colour + padded + term.Reset
			}
		}
		return padded
	})
}

// Rest is the columns a narrow table moves off the row, each with its heading; "" when the table
// is not narrow.
func (t Table) Rest(r Row) string {
	if !t.narrow {
		return ""
	}
	cells := r.cells()
	parts := make([]string, 0, columns-narrowColumns+1)
	if r.Kind == KindTeam {
		parts = append(parts, aboutHeading+" "+cells[colMember])
	}
	for i := narrowColumns; i < columns; i++ {
		parts = append(parts, headings[i]+" "+cells[i])
	}
	return strings.Join(parts, "   ")
}

// measure sets each column's width to its longest value; a team's description counts only when
// descriptions stay on their rows.
func measure(rows []Row, descriptions bool) [columns]int {
	var widths [columns]int
	for i, h := range headings {
		widths[i] = ui.Width(h)
	}
	for _, r := range rows {
		for i, c := range r.cells() {
			if i == colMember && r.Kind == KindTeam && !descriptions {
				continue
			}
			widths[i] = max(widths[i], ui.Width(c))
		}
	}
	return widths
}

// lineWidth is how wide a line of the first n columns is.
func (t Table) lineWidth(n int) int {
	width := len(gap) * (n - 1)
	for _, w := range t.widths[:n] {
		width += w
	}
	return width
}

// shown is how many columns are on a row.
func (t Table) shown() int {
	if t.narrow {
		return narrowColumns
	}
	return columns
}

// shownCells are the row's words as shown: a narrow table's team row has its description in
// Rest, not in MEMBER.
func (t Table) shownCells(r Row) [columns]string {
	cells := r.cells()
	if t.narrow && r.Kind == KindTeam {
		cells[colMember] = "-"
	}
	return cells
}

// line joins the shown columns of cells, each padded to its width (the last not at all) and then
// passed through dress, which may wrap it in colour codes.
func (t Table) line(cells [columns]string, dress func(i int, padded string) string) string {
	n := t.shown()
	var b strings.Builder
	for i, c := range cells[:n] {
		if i > 0 {
			b.WriteString(gap)
		}
		padded := c
		if i < n-1 {
			padded += strings.Repeat(" ", max(0, t.widths[i]-ui.Width(c)))
		}
		b.WriteString(dress(i, padded))
	}
	return b.String()
}

// wordColour is the colour a status word is shown in, or "" for none.
func wordColour(word string) string {
	switch {
	case isOneOf(word, "free", "invited", "no", "parked", "deferred", "never signed in"), strings.HasSuffix(word, "(stopped)"):
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
