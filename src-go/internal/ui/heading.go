package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrNoHeading is a question or pick asked for without saying where the person is.
var ErrNoHeading = errors.New("ui: a screen with no heading: say where the person is (its crumbs) before asking")

// crumbSeparator joins the crumbs of a trail.
const crumbSeparator = " › "

// The bounds of the rule drawn under a heading, in columns.
const (
	shortestRule = 24
	longestRule  = 72
)

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

// check refuses a heading that names no place: no crumbs, or an empty one.
func (h Heading) check() error {
	if len(h.Crumbs) == 0 || slices.Contains(h.Crumbs, "") {
		return fmt.Errorf("%w (crumbs %q)", ErrNoHeading, h.Crumbs)
	}
	return nil
}

// render is the heading as it is drawn above every question: the trail, the context line when
// there is one, and a rule. In colour the crumbs are bold in the accent.
func (h Heading) render(colour bool) string {
	trail := h.Trail()
	width := utf8.RuneCountInString(trail)
	first := trail
	if colour {
		first = headingColour + strings.Join(h.Crumbs, crumbSeparator) + colourReset + h.stepText()
	}
	lines := []string{first}
	if h.Context != "" {
		lines = append(lines, h.Context)
		width = max(width, utf8.RuneCountInString(h.Context))
	}
	return strings.Join(append(lines, strings.Repeat("─", min(max(width, shortestRule), longestRule))), "\n")
}

// above is text with the heading drawn on top of it.
func (h Heading) above(text string, colour bool) string {
	if text == "" {
		return h.render(colour)
	}
	return h.render(colour) + "\n" + text
}
