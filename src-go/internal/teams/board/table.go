// Package board is the table of teams, seats and people ccy-teams shows: one pass over the
// machine (systemd, tmux, the session registry, the declaration, agent-bus) read into a
// Snapshot of rows, and the --list flow that prints it. It owns what a row says and how it is
// painted; what to do about a row is the picker's.
package board

import (
	"slices"
	"strings"
	"unicode/utf8"

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

// Row is one line of the table. Member is the seat (a ccy seat with a checkout), the handle (a
// member with none) or the person, as keys and flows name it; the MEMBER column shows a member's
// whole handle instead (memberCell). Handle and Dir are what a key pressed on the row acts on;
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

// descriptionWidth is how much of a team's description the MEMBER column makes room for on a
// terminal: a longer one is cut to the column (piped, --list prints it whole). A member's handle
// is never cut; the column is as wide as the longest.
const descriptionWidth = 36

// Label is the row's member or person as a person is shown it, in the MEMBER column and in every
// menu and message about the row: a member's whole handle with the coordinator's role beside it
// (agentbus.Label), a person's name. A team row's is its description.
func (r Row) Label() string {
	if r.Kind == KindTeam || r.Kind == KindHuman || r.Handle == "" || r.Handle == "-" {
		return r.Member
	}
	return agentbus.Label(r.Handle, r.Role)
}

// cells are the row's columns as words.
func (r Row) cells() [columns]string {
	return [columns]string{r.Team, r.Label(), string(r.Kind), r.State, r.Applied, r.Session, r.Restore, r.Where}
}

// Table is a set of rows laid out together: each column as wide as its longest value, so no row
// shifts the columns after a long one. On a terminal too narrow for every column, the table is
// narrow: the rows keep TEAM, MEMBER, KIND and STATE whole, and the other columns move off the
// row to Rest, which the picker shows in the row's header and --list on the line below it.
type Table struct {
	widths [columns]int
	// cut is a terminal: a team's description is cut to the MEMBER column.
	cut, narrow bool
}

// Layout lays rows out for a terminal width columns wide; 0 is not a terminal (piped), with no
// limit and descriptions whole.
func Layout(rows []Row, width int) Table {
	t := Table{cut: width > 0}
	for i, h := range headings {
		t.widths[i] = runes(h)
	}
	for _, r := range rows {
		for i, c := range r.cells() {
			n := runes(c)
			if i == colMember && r.Kind == KindTeam {
				n = min(n, descriptionWidth)
			}
			t.widths[i] = max(t.widths[i], n)
		}
	}
	t.narrow = width > 0 && t.lineWidth(columns) > width
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
	parts := make([]string, 0, columns-narrowColumns)
	for i := narrowColumns; i < columns; i++ {
		parts = append(parts, headings[i]+" "+cells[i])
	}
	return strings.Join(parts, "   ")
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

// shownCells are the row's words as shown: a team's description cut to the MEMBER column on a
// terminal.
func (t Table) shownCells(r Row) [columns]string {
	cells := r.cells()
	if t.cut && r.Kind == KindTeam {
		cells[colMember] = cut(cells[colMember], t.widths[colMember])
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
			padded += strings.Repeat(" ", max(0, t.widths[i]-runes(c)))
		}
		b.WriteString(dress(i, padded))
	}
	return b.String()
}

// cut is text shortened to width characters, ending in an ellipsis when it was longer.
func cut(text string, width int) string {
	r := []rune(text)
	if len(r) <= width {
		return text
	}
	return string(r[:width-1]) + "…"
}

func runes(s string) int { return utf8.RuneCountInString(s) }

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
