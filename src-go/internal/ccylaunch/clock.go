package ccylaunch

import (
	"io/fs"
	"time"
)

// Clock is the time the front answers by: now, and a file's modification time on the same
// clock (the usage cache's age). cmd/claude-yolo gives the front SystemClock; only a build with
// the difftest tag gives it the harness's pinned clock, so no environment moves the clock of
// the installed program.
type Clock interface {
	Now() time.Time
	ModTime(info fs.FileInfo) time.Time
}

// SystemClock is the machine's clock, in the local zone.
type SystemClock struct{}

// Now is the machine's now.
func (SystemClock) Now() time.Time { return time.Now() }

// ModTime is the time the file system gives the file.
func (SystemClock) ModTime(info fs.FileInfo) time.Time { return info.ModTime() }

// clock is the front's Clock, the system's when none was given.
func (f Front) clock() Clock {
	if f.Clock == nil {
		return SystemClock{}
	}
	return f.Clock
}
