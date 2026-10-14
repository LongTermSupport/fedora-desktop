package all

import (
	"bytes"
	"io"
	"regexp"
	"strings"
)

// verdictShape is a line that opens the way the run's own closing verdict does ("✓ QA passed:",
// "✗ QA FAILED:"). A stage name is lower-case, so no stage line has this shape.
var verdictShape = regexp.MustCompile(`^[✓✗⚠] QA `)

// quarantineIndent is put before such a line when it comes from a gate rather than from qa-all.
const quarantineIndent = "  "

// quarantineText indents every line of a gate's output that opens like the run's verdict, so
// the verdict qa-all prints last is the only line of the output that does (SPEC-qa-all.md B23).
// Everything else is left byte for byte: stage lines keep their symbol in column one, which is
// where verdicts.py reads them.
func quarantineText(text string) string {
	var out strings.Builder
	for line := range strings.SplitAfterSeq(text, "\n") {
		if verdictShape.MatchString(line) {
			out.WriteString(quarantineIndent)
		}
		out.WriteString(line)
	}
	return out.String()
}

// quarantineWriter is quarantineText for a stream: it holds a partial line until its newline
// arrives, so a line split across writes is judged whole. Flush writes what is held.
type quarantineWriter struct {
	to      io.Writer
	pending []byte
}

// Write implements io.Writer. It reports all of p consumed unless the underlying write fails.
func (w *quarantineWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	if i := bytes.LastIndexByte(w.pending, '\n'); i >= 0 {
		whole := w.pending[:i+1]
		w.pending = bytes.Clone(w.pending[i+1:])
		if _, err := io.WriteString(w.to, quarantineText(string(whole))); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes the final line, which has no newline.
func (w *quarantineWriter) Flush() error {
	rest := string(w.pending)
	w.pending = nil
	_, err := io.WriteString(w.to, quarantineText(rest))
	return err
}
