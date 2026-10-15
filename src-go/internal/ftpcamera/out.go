package ftpcamera

import (
	"fmt"
	"io"
	"sync"
)

// palette is the escape codes a line is painted with: bold, green, yellow, cyan, red, and the
// reset. A stream that is not a terminal gets the empty palette, so the text is the same (E10).
type palette struct{ b, g, y, c, r, n string }

func colours(on bool) palette {
	if !on {
		return palette{}
	}
	return palette{b: "\x1b[1m", g: "\x1b[0;32m", y: "\x1b[1;33m", c: "\x1b[0;36m", r: "\x1b[0;31m", n: "\x1b[0m"}
}

// lockedWriter serialises the writes of the session's goroutines (the log printer, the
// monitor, the teardown) onto one stream, and keeps the first write that failed.
type lockedWriter struct {
	mu     *sync.Mutex
	w      io.Writer
	failed *error
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.w.Write(p)
	if err != nil && *l.failed == nil {
		*l.failed = err
	}
	return n, err
}

// stream is one of the two output streams with its palette.
type stream struct {
	palette

	w io.Writer
}

// f writes one formatted piece of text. A failed write is kept by the lockedWriter beneath
// and turns a successful run into a failure when it ends.
func (s stream) f(format string, args ...any) {
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil {
		return
	}
}
