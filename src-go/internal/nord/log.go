package nord

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLogSize is the size past which the log is rotated to nord.log.old (E21).
const maxLogSize = 1 << 20

// runLog is the log a changing command appends its lines to, each stamped (E21). It keeps the
// first write that failed, so the run can fail for it when it ends.
type runLog struct {
	w   io.WriteCloser
	now func() time.Time
	err error
}

// openLog opens the log for appending before a changing command acts (E21); false after saying
// why it could not.
func (r *run) openLog() bool {
	path := r.path(logRel)
	w, err := openLogFile(path)
	if err != nil {
		r.failf("cannot write the log %s: %v", r.abbrev(path), err)
		return false
	}
	r.log = &runLog{w: w, now: r.env.Now}
	return true
}

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("make its directory: %w", err)
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info.Size() > maxLogSize:
		if renameErr := os.Rename(path, path+".old"); renameErr != nil {
			return nil, fmt.Errorf("rotate it: %w", renameErr)
		}
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("read its size: %w", err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open it: %w", err)
	}
	return f, nil
}

// line appends text, each of its lines stamped with the time.
func (l *runLog) line(text string) {
	if l.err != nil {
		return
	}
	stamp := l.now().Format(time.RFC3339)
	for part := range strings.SplitSeq(text, "\n") {
		if _, err := fmt.Fprintf(l.w, "%s %s\n", stamp, part); err != nil {
			l.err = err
			return
		}
	}
}

// close closes the log, and is the first write that failed, or the close's own error.
func (l *runLog) close() error {
	return errors.Join(l.err, l.w.Close())
}
