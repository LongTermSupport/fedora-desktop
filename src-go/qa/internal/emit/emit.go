// Package emit is how a QA gate writes its two streams: an Emitter remembers the first write
// error instead of losing it, and Finish turns a stream that could not be written into the
// gate's "cannot run" exit code. Every gate used to carry its own copy.
package emit

import (
	"fmt"
	"io"
	"strings"
)

// Emitter writes to one stream and keeps the first error that stream gave.
type Emitter struct {
	w   io.Writer
	err error
}

// New is an Emitter over w.
func New(w io.Writer) *Emitter { return &Emitter{w: w} }

// Err is the first write error, or nil.
func (e *Emitter) Err() error { return e.err }

// Line writes text and a newline.
func (e *Emitter) Line(text string) {
	if _, err := fmt.Fprintln(e.w, text); err != nil && e.err == nil {
		e.err = err
	}
}

// Text writes text as it is.
func (e *Emitter) Text(text string) {
	if _, err := io.WriteString(e.w, text); err != nil && e.err == nil {
		e.err = err
	}
}

// Write makes an Emitter an io.Writer for code that streams into it.
func (e *Emitter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil && e.err == nil {
		e.err = err
	}
	return n, err
}

// Block writes a multi-line text (a usage text) ending in exactly one newline.
func (e *Emitter) Block(text string) {
	e.Line(strings.TrimSuffix(text, "\n"))
}

// Finish is a gate's exit code once its run is over. A stdout that could not be written is said
// on stderr and is cannotRun; a stderr that could not be written turns every other code into
// cannotRun, because the findings were lost.
func Finish[C comparable](out, errOut *Emitter, code, cannotRun C) C {
	if out.err != nil {
		errOut.Line("ERROR: cannot write to stdout: " + out.err.Error())
		return cannotRun
	}
	if code != cannotRun && errOut.err != nil {
		return cannotRun
	}
	return code
}
