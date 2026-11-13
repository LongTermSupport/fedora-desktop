// Package term writes a tool's lines; SecretScreen.Show is the one display of a secret.
package term

import (
	"fmt"
	"io"
)

// Printer writes to the streams a tool is given.
type Printer struct{ err error }

// Print writes s to w: a terminal write of its second argument, through io.WriteString.
func (p *Printer) Print(w io.Writer, s string) { // want Print:"writes parameters 1"
	if _, err := io.WriteString(w, s); err != nil {
		p.err = err
	}
}

// Term writes human-facing lines to Err.
type Term struct {
	Err io.Writer
	bad error
}

// Printf writes to Err.
func (t *Term) Printf(format string, args ...any) { // want Printf:"writes parameters 0 1"
	if _, err := fmt.Fprintf(t.Err, format, args...); err != nil {
		t.bad = err
	}
}

// Say is a heading line.
func (t *Term) Say(text string) { t.Printf("%s\n", text) } // want Say:"writes parameters 0"

// SecretScreen is the secret display: its own write of text is the one the rule does not judge.
type SecretScreen struct {
	W    io.Writer
	open bool
	err  error
}

// Show writes text on the alternate screen.
func (s *SecretScreen) Show(text string) {
	if !s.open {
		s.write("\x1b[?1049h")
		s.open = true
	}
	s.write(text)
}

// Write is Show as an io.Writer.
func (s *SecretScreen) Write(p []byte) (int, error) {
	s.Show(string(p))
	return len(p), s.err
}

func (s *SecretScreen) write(text string) { // want write:"writes parameters 0"
	if _, err := io.WriteString(s.W, text); err != nil {
		s.err = err
	}
}

// AbbrevHome writes nothing: a secret passed to it is not a write.
func AbbrevHome(path, home string) string { return path + home } // want AbbrevHome:"returns parameters 0 1"
