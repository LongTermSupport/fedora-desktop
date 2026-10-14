package gobuild

import (
	"fmt"
	"io"
)

// sink is a writer that remembers its first failed write, so the output can be written
// plainly and the failure reported once.
type sink struct {
	w   io.Writer
	err error
}

func (s *sink) printf(format string, args ...any) {
	if s.err == nil {
		_, s.err = fmt.Fprintf(s.w, format, args...)
	}
}

// settle is the exit code once the output is written: a verdict that could not be delivered
// is a failure, whatever the verdict was.
func settle(code int, sinks ...*sink) int {
	for _, s := range sinks {
		if s.err != nil && code == ExitOK {
			return ExitFailure
		}
	}
	return code
}
