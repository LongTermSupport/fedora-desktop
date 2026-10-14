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

// ExitCode is a tool's exit status. Every run function returns one; only a main turns it
// into the int os.Exit takes.
type ExitCode int

// Exit codes shared by ccy-teams, ccy-sessions and ccy.
const (
	ExitOK        ExitCode = 0
	ExitFailure   ExitCode = 1
	ExitUsage     ExitCode = 64
	ExitCancelled ExitCode = 130
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

	writeErr error
}

// WriteFailed is the first failed write to Err, nil if every write landed. A tool checks it
// when it ends: output that could not be shown is a failure, not a success.
func (t *Term) WriteFailed() error { return t.writeErr }

// New is a Term on the process's stderr, reading the real environment.
func New() *Term {
	return &Term{Err: os.Stderr, IsTerminal: FileIsTerminal, Getenv: os.Getenv}
}

// FileIsTerminal reports whether v is an *os.File attached to a terminal: a character device
// that is not /dev/null.
func FileIsTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

// YesNo is a flag's word in the tools' record files: "yes" or "no".
func YesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ColourOn reports whether w gets colour: only a terminal, and never with NO_COLOR
// (no-color.org), so piped output stays plain text.
func (t *Term) ColourOn(w io.Writer) bool {
	return t.IsTerminal(w) && t.Getenv("NO_COLOR") == ""
}

// Finish is the exit code a tool returns: code, unless the run reported success while its
// output could not be written (to stdout, whose first failure is stdoutErr, or to Err), which
// is a failure. A run that already failed keeps its own, more specific, code.
func (t *Term) Finish(code ExitCode, stdoutErr error) ExitCode {
	if code != ExitOK {
		return code
	}
	if stdoutErr != nil {
		t.PrintError("could not write the output: " + stdoutErr.Error())
		return ExitFailure
	}
	if t.writeErr != nil {
		return ExitFailure
	}
	return code
}

// Printf writes to Err. A failed write to the terminal cannot be reported anywhere
// better, so the first one is kept for the tool to read with WriteFailed when it ends.
func (t *Term) Printf(format string, args ...any) {
	if _, err := fmt.Fprintf(t.Err, format, args...); err != nil && t.writeErr == nil {
		t.writeErr = err
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
