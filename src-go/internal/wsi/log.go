package wsi

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLogSize is the debug log size beyond which it is renamed to debug.log.old (W7).
const maxLogSize = 1 << 20

// logger is W7: lines on stderr and in the debug log, held in memory unless -d or -v.
type logger struct {
	stderr    io.Writer
	file      string
	now       func() time.Time
	immediate bool // -d or -v: each file line written at once, debug lines shown
	held      []string
	warned    bool
}

func (l *logger) stamp(msg string) string {
	return "[" + l.now().Format("2006-01-02T15:04:05") + "] [WSI] " + msg
}

// say writes one line to stderr. Nothing better can be done with a stderr that fails.
func (l *logger) say(line string) {
	if _, err := io.WriteString(l.stderr, line+"\n"); err != nil {
		return
	}
}

// toFile records msg in the log: at once with -d or -v, otherwise held.
func (l *logger) toFile(msg string) {
	if l.immediate {
		l.write([]string{l.stamp(msg)})
		return
	}
	l.held = append(l.held, l.stamp(msg))
}

// direct writes msg to the log now, whatever the mode.
func (l *logger) direct(msg string) { l.write([]string{l.stamp(msg)}) }

func (l *logger) info(msg string) {
	l.say("[WSI] " + msg)
	l.toFile(msg)
}

func (l *logger) debug(msg string) {
	if l.immediate {
		l.say("[DEBUG] " + msg)
		l.toFile("[DEBUG] " + msg)
	}
}

func (l *logger) errorLine(msg string) {
	l.say("[ERROR] " + msg)
	l.toFile("[ERROR] " + msg)
}

// flush writes the held lines and says how many there were.
func (l *logger) flush() int {
	n := len(l.held)
	if n > 0 {
		l.write(l.held)
		l.held = nil
	}
	return n
}

// write appends lines to the log, creating its directory and rotating it first. A log that
// cannot be written is said once on stderr and the dictation goes on (B12).
func (l *logger) write(lines []string) {
	if err := l.append(lines); err != nil && !l.warned {
		l.warned = true
		l.say("[WSI] cannot write the log " + l.file + ": " + err.Error())
	}
}

func (l *logger) append(lines []string) error {
	if err := os.MkdirAll(filepath.Dir(l.file), 0o750); err != nil {
		return fmt.Errorf("create its directory: %w", err)
	}
	if info, err := os.Stat(l.file); err == nil && info.Size() > maxLogSize {
		if err := os.Rename(l.file, l.file+".old"); err != nil {
			return fmt.Errorf("rotate: %w", err)
		}
	}
	f, err := os.OpenFile(l.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if _, err := io.WriteString(f, strings.Join(lines, "\n")+"\n"); err != nil {
		if cerr := f.Close(); cerr != nil {
			return fmt.Errorf("write: %w (and close: %w)", err, cerr)
		}
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}
