package ui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// ErrNoHeading is a question or pick asked for without saying where the person is.
var ErrNoHeading = errors.New("ui: a screen with no heading: say where the person is (its crumbs) before asking")

// crumbSeparator joins the crumbs of a trail.
const crumbSeparator = " › "

// shortestRule is the shortest rule drawn under a heading, in columns (never wider than the
// heading's width).
const shortestRule = 24

// headingColour is the crumbs' colour: bold in the accent (Accent, terminal index 3).
const (
	headingColour = "\x1b[1;3" + Accent + "m"
	colourReset   = "\x1b[0m"
)

// Heading is what every screen says first, the same way everywhere: where the person is (the
// crumbs, and the step of a wizard) and what the screen acts on (the context line, with the
// answers given so far). Every question and pick takes one, and refuses to draw without it.
type Heading struct {
	// Crumbs is the path to the screen, outermost first: "Teams", "alpha", "Add LXC member".
	Crumbs []string
	// Step and Steps, when Steps is set, are the step of a wizard ("step 3/4"), and StepName
	// what that step asks.
	Step, Steps int
	StepName    string
	// Context is one line about what the screen acts on: "container-a · user agent".
	Context string
}

// Here is a heading at the path crumbs.
func Here(crumbs ...string) Heading { return Heading{Crumbs: slices.Clone(crumbs)} }

// Push is the heading of a screen opened from this one: its crumbs and then crumbs, with no
// step of its own and the same context.
func (h Heading) Push(crumbs ...string) Heading {
	return Heading{Crumbs: append(slices.Clone(h.Crumbs), crumbs...), Context: h.Context}
}

// AtStep is the heading at step n of m, which asks name.
func (h Heading) AtStep(n, m int, name string) Heading {
	h.Crumbs = slices.Clone(h.Crumbs)
	h.Step, h.Steps, h.StepName = n, m, name
	return h
}

// About is the heading with context as its context line.
func (h Heading) About(context string) Heading {
	h.Crumbs = slices.Clone(h.Crumbs)
	h.Context = context
	return h
}

// Trail is the crumbs and the step on one line: "Teams › alpha › Add LXC member · step 3/4
// Checkout".
func (h Heading) Trail() string { return strings.Join(h.Crumbs, crumbSeparator) + h.stepText() }

// Draw is the heading as a ui screen of its own drawing (a Bubble Tea component) shows it:
// wrapped to width, its crumbs in colour when colour is set. A heading that names no place is
// ErrNoHeading.
func (h Heading) Draw(colour bool, width int) (string, error) {
	if err := h.check(); err != nil {
		return "", err
	}
	return h.render(colour, width), nil
}

func (h Heading) stepText() string {
	if h.Steps == 0 {
		return ""
	}
	text := fmt.Sprintf(" · step %d/%d", h.Step, h.Steps)
	if h.StepName != "" {
		text += " " + h.StepName
	}
	return text
}

// check refuses a heading that names no place: no crumbs, or an empty or blank one.
func (h Heading) check() error {
	blank := func(c string) bool { return strings.TrimSpace(c) == "" }
	if len(h.Crumbs) == 0 || slices.ContainsFunc(h.Crumbs, blank) {
		return fmt.Errorf("%w (crumbs %q)", ErrNoHeading, h.Crumbs)
	}
	return nil
}

// piece is one part of a heading line: its text and the separator that joins it to the part
// before it ("" for the first). A crumb is drawn in the accent.
type piece struct {
	sep, text string
	crumb     bool
}

// line is one drawn line of a heading: its crumbs (drawn in the accent), then the rest.
type line struct{ crumbs, rest string }

func (l line) plain() string { return l.crumbs + l.rest }

func (l line) draw(colour bool) string {
	if !colour || l.crumbs == "" {
		return l.plain()
	}
	return headingColour + l.crumbs + colourReset + l.rest
}

// wrap lays pieces out in lines of at most width runes, never cutting anything off: fzf and gum
// truncate a header line that is too wide, so the heading is wrapped before they see it. A
// piece that does not fit on the line starts the next one, led by its separator's mark ("› ",
// "· "); a piece longer than a whole line is cut into lines of width.
func wrap(pieces []piece, width int) []line {
	var lines []line
	cur, n := line{}, 0
	add := func(text string, crumb bool) {
		if crumb {
			cur.crumbs += text
		} else {
			cur.rest += text
		}
		n += utf8.RuneCountInString(text)
	}
	for _, p := range pieces {
		text := p.sep + p.text
		if n == 0 || n+utf8.RuneCountInString(text) > width {
			if n > 0 {
				lines, cur, n = append(lines, cur), line{}, 0
			}
			text = strings.TrimLeft(p.sep, " ") + p.text
			if utf8.RuneCountInString(text) > width {
				text = p.text // the mark would push a piece that fits a line onto two
			}
		}
		for room := width - n; utf8.RuneCountInString(text) > room; room = width - n {
			r := []rune(text)
			add(string(r[:room]), p.crumb)
			text = string(r[room:])
			lines, cur, n = append(lines, cur), line{}, 0
		}
		if text != "" {
			add(text, p.crumb)
		}
	}
	if n > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// pieces is the trail as pieces: each crumb, then the step.
func (h Heading) pieces() []piece {
	out := make([]piece, 0, len(h.Crumbs)+1)
	for i, c := range h.Crumbs {
		sep := crumbSeparator
		if i == 0 {
			sep = ""
		}
		out = append(out, piece{sep: sep, text: c, crumb: true})
	}
	if step := h.stepText(); step != "" {
		out = append(out, piece{sep: contextSeparator, text: strings.TrimPrefix(step, contextSeparator)})
	}
	return out
}

// contextSeparator joins the parts of a context line, and the step to the crumbs.
const contextSeparator = " · "

// render is the heading as it is drawn above every question, in lines of at most width runes:
// the trail, the context line when there is one, and a rule as long as the longest line. In
// colour the crumbs are bold in the accent.
func (h Heading) render(colour bool, width int) string {
	width = max(width, 1)
	lines := wrap(h.pieces(), width)
	if h.Context != "" {
		parts := strings.Split(h.Context, contextSeparator)
		ctx := make([]piece, 0, len(parts))
		for i, part := range parts {
			sep := contextSeparator
			if i == 0 {
				sep = ""
			}
			ctx = append(ctx, piece{sep: sep, text: part})
		}
		lines = append(lines, wrap(ctx, width)...)
	}
	longest := 0
	drawn := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		longest = max(longest, utf8.RuneCountInString(l.plain()))
		drawn = append(drawn, l.draw(colour))
	}
	return strings.Join(append(drawn, strings.Repeat("─", min(max(longest, shortestRule), width))), "\n")
}

// above is text with the heading drawn on top of it, in lines of at most width runes.
func (h Heading) above(text string, colour bool, width int) string {
	if text == "" {
		return h.render(colour, width)
	}
	return h.render(colour, width) + "\n" + text
}

// The width a heading is wrapped to: the terminal's, less what a screen's frame and padding
// take, or a common width when the terminal's cannot be read.
const (
	headingMargin        = 10
	fallbackHeadingWidth = 68
	narrowestHeading     = 16
)

// terminalColumns is the terminal's width, 0 when it cannot be read; a variable so a test
// stands in for the terminal.
var terminalColumns = readTerminalColumns

// readTerminalColumns is $COLUMNS when it is a number, else the width of the first of stderr,
// stdin and stdout that is a terminal, else 0.
func readTerminalColumns() int {
	if cols, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && cols > 0 {
		return cols
	}
	for _, f := range []*os.File{os.Stderr, os.Stdin, os.Stdout} {
		if ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ); err == nil && ws.Col > 0 {
			return int(ws.Col)
		}
	}
	return 0
}

// headingWidth is the width a heading is wrapped to on a terminal columns wide (0: read it).
func headingWidth(columns int) int {
	if columns <= 0 {
		columns = terminalColumns()
	}
	if columns <= 0 {
		return fallbackHeadingWidth
	}
	return max(columns-headingMargin, narrowestHeading)
}
