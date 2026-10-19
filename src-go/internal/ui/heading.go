package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
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

// wrapper lays pieces out in lines of at most width cells.
type wrapper struct {
	width int
	lines []line
	cur   line
	n     int // cells on the current line
}

func (w *wrapper) add(text string, crumb bool) {
	if crumb {
		w.cur.crumbs += text
	} else {
		w.cur.rest += text
	}
	w.n += Width(text)
}

func (w *wrapper) newLine() {
	if w.n > 0 {
		w.lines, w.cur, w.n = append(w.lines, w.cur), line{}, 0
	}
}

// place puts one piece on the lines. It joins the line after its separator when it fits;
// otherwise it starts the next line led by its separator's mark ("› ", "· "), or, when the
// mark and the piece do not fit a whole line but the piece does, the mark ends the line before
// it. A piece wider than a whole line is cut between grapheme clusters, never elided.
func (w *wrapper) place(p piece) {
	if text := p.sep + p.text; w.n == 0 || w.n+Width(text) <= w.width {
		if w.n == 0 {
			text = p.text
		}
		w.fill(text, p.crumb, 0)
		return
	}
	mark := strings.TrimSpace(p.sep)
	if Width(mark+" "+p.text) > w.width && Width(p.text) <= w.width && w.n+1+Width(mark) <= w.width {
		w.add(" "+mark, p.crumb)
		w.newLine()
		w.fill(p.text, p.crumb, 0)
		return
	}
	w.newLine()
	w.fill(mark+" "+p.text, p.crumb, len(mark))
}

// fill adds text to the current line, carrying it onto further lines where it is wider than
// what is left: at its last space that fits (past the first lead bytes, so a mark is never
// left alone on a line), else between grapheme clusters.
func (w *wrapper) fill(text string, crumb bool, lead int) {
	for Width(text) > w.width-w.n {
		head := ansi.Truncate(text, w.width-w.n, "")
		rest := text[len(head):]
		if at := strings.LastIndex(head, " "); at > lead {
			head, rest = head[:at], text[at+1:]
		}
		if head == "" { // a cluster wider than the line: it gets a line of its own
			head = string([]rune(text)[0])
			rest = text[len(head):]
		}
		w.add(head, crumb)
		text = rest
		w.newLine()
	}
	if text != "" {
		w.add(text, crumb)
	}
}

// wrap lays pieces out in lines of at most width cells, never cutting anything off: fzf
// truncates a header line wider than its window, so the heading is wrapped before it is seen.
func wrap(pieces []piece, width int) []line {
	w := &wrapper{width: width}
	for _, p := range pieces {
		w.place(p)
	}
	w.newLine()
	return w.lines
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

// contextPieces is the context line as pieces, split at its separators; an empty part is left
// out, so it draws no stray mark.
func (h Heading) contextPieces() []piece {
	parts := strings.Split(h.Context, contextSeparator)
	out := make([]piece, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		sep := contextSeparator
		if len(out) == 0 {
			sep = ""
		}
		out = append(out, piece{sep: sep, text: part})
	}
	return out
}

// contextSeparator joins the parts of a context line, and the step to the crumbs.
const contextSeparator = " · "

// render is the heading as it is drawn above every question, in lines of at most width cells:
// the trail, the context line when there is one, and a rule as long as the longest line. In
// colour the crumbs are bold in the accent.
func (h Heading) render(colour bool, width int) string {
	width = max(width, 2)
	lines := append(wrap(h.pieces(), width), wrap(h.contextPieces(), width)...)
	longest := 0
	drawn := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		longest = max(longest, Width(l.plain()))
		drawn = append(drawn, l.draw(colour))
	}
	return strings.Join(append(drawn, strings.Repeat("─", min(max(longest, shortestRule), width))), "\n")
}

// above is text with the heading drawn on top of it, in lines of at most width cells.
func (h Heading) above(text string, colour bool, width int) string {
	if text == "" {
		return h.render(colour, width)
	}
	return h.render(colour, width) + "\n" + text
}

// What a screen takes from the terminal's width before its header, in columns, measured on a
// pseudo-terminal against fzf with the house frame: the framed picker loses RowChrome (its
// margin, border, padding and fzf's pointer and marker columns) and two more; Picker.Choose has
// no frame, only fzf's columns and two more; a gum header is printed as it is, kept off the
// last column.
const (
	framedMargin = RowChrome + 2
	plainMargin  = 4
	gumMargin    = 2
)

// headingWidth is the width a gum question's heading is wrapped to.
func (g *Gum) headingWidth(ctx context.Context) int { return g.columns(ctx) - gumMargin }

// headingWidth is the width a picker's heading is wrapped to, on a screen that takes margin.
func (p *Picker) headingWidth(ctx context.Context, margin int) int {
	return columns(ctx, p.Runner, nil, p.Stderr) - margin
}
