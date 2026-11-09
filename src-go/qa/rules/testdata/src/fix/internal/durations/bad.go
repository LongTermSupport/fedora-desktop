// Package durations is the fixture of project_duration_overflow: durations built from numbers
// read from input, flagged where no bound keeps the result inside int64 nanoseconds.
package durations

import (
	"strconv"
	"time"
)

// pruneCutoff is the originating defect: vmtest prune-runs --older-than 200000 made the cutoff
// negative, so every failed run was older than it.
func pruneCutoff(now, started time.Time, days int) bool {
	return now.Sub(started) > time.Duration(days)*24*time.Hour // want `time.Duration from a number with no bound`
}

// unitFirst is the other operand order.
func unitFirst(secs int) time.Duration {
	return time.Second * time.Duration(secs) // want `time.Duration from a number with no bound`
}

// parsed is a number from the command line, read and never bounded.
func parsed(s string) (time.Duration, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, err
	}
	return time.Duration(n) * time.Minute, nil // want `time.Duration from a number with no bound`
}

// nonConstantUnit multiplies by a unit set at run time, which no constant bound can cap.
func nonConstantUnit(minutes int, minute time.Duration) time.Duration {
	if minutes > 10 {
		return 0
	}
	return time.Duration(minutes) * minute // want `time.Duration from a number with no bound`
}

// acceptShape compares with a constant, but as a test of the value it keeps: nothing above
// the constant is refused.
func acceptShape(n int) time.Duration {
	if n <= 60 {
		println("short")
	}
	return time.Duration(n) * time.Second // want `time.Duration from a number with no bound`
}

// warnOnly compares as a refusal would, then goes on with the number.
func warnOnly(n int) time.Duration {
	if n > 60 {
		println("long")
	}
	return time.Duration(n) * time.Second // want `time.Duration from a number with no bound`
}

// field is a struct field with no bound anywhere in the package.
type field struct{ grace int }

func (f field) wait() time.Duration {
	return time.Duration(f.grace) * time.Second // want `time.Duration from a number with no bound`
}
