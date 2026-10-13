// Package term is the small terminal-output layer the ccy tools share: colour only on a
// terminal and never with NO_COLOR, heading and step lines on stderr, the "~" home
// abbreviation, and the exit codes every tool returns.
package term

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Exit codes shared by ccy-teams, ccy-sessions and ccy.
const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 64
	ExitCancelled = 130
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	green  = "\x1b[32m"
	red    = "\x1b[0;31m"
	accent = "\x1b[33m" // terminal index 3: the theme decides its shade
)

// Term writes a tool's human-facing lines to Err. IsTerminal and Getenv are seams so a
// test decides what is a terminal and what NO_COLOR says.
type Term struct {
	Err        io.Writer
	IsTerminal func(v any) bool
	Getenv     func(key string) string

	// Steps is how many questions this run asks; Step counts up to it.
	Steps int
	step  int

	// WriteErr is the first failed write to Err, if any.
	WriteErr error
}

// New is a Term on the process's stderr, reading the real environment.
func New() *Term {
	return &Term{Err: os.Stderr, IsTerminal: FileIsTerminal, Getenv: os.Getenv}
}

// FileIsTerminal reports whether v is an *os.File attached to a character device.
func FileIsTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ColourOn reports whether w gets colour: only a terminal, and never with NO_COLOR
// (no-color.org), so piped output stays plain text.
func (t *Term) ColourOn(w io.Writer) bool {
	return t.IsTerminal(w) && t.Getenv("NO_COLOR") == ""
}

// Printf writes to Err. A failed write to the terminal cannot be reported anywhere
// better, so the first one is kept in WriteErr for the tool to check when it ends.
func (t *Term) Printf(format string, args ...any) {
	if _, err := fmt.Fprintf(t.Err, format, args...); err != nil && t.WriteErr == nil {
		t.WriteErr = err
	}
}

// Say is a heading line, bold on a terminal.
func (t *Term) Say(text string) {
	if t.ColourOn(t.Err) {
		text = bold + text + reset
	}
	t.Printf("%s\n", text)
}

// PrintError prints the message literally; only the ERROR: label is coloured, and only
// on a terminal.
func (t *Term) PrintError(msg string) {
	label := "ERROR:"
	if t.ColourOn(t.Err) {
		label = red + label + reset
	}
	t.Printf("%s %s\n", label, msg)
}

// Step is a question's numbered heading, after a blank line.
func (t *Term) Step(title string) {
	t.step++
	t.Printf("\n")
	if t.ColourOn(t.Err) {
		t.Printf("%s%s %d/%d  %s%s\n", bold, accent, t.step, t.Steps, title, reset)
		return
	}
	t.Printf("%d/%d  %s\n", t.step, t.Steps, title)
}

// Answered keeps the answer on screen under its step once the prompt has gone.
func (t *Term) Answered(value string) {
	mark := "✓"
	if t.ColourOn(t.Err) {
		mark = green + mark + reset
	}
	t.Printf("     %s %s\n", mark, value)
}

// AbbrevHome shows a path under home as "~/...".
func AbbrevHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
