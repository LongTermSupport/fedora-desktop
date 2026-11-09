// Package duration multiplies a time.Duration by a number and refuses a product that leaves
// int64 nanoseconds, where Go's arithmetic would wrap it negative (project_duration_overflow).
package duration

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrTooLong is a product that is negative or past the longest time.Duration.
var ErrTooLong = errors.New("longer than a time.Duration can hold")

// Times is n units. A negative n or unit, or a product past math.MaxInt64 nanoseconds, is
// ErrTooLong.
func Times(n int, unit time.Duration) (time.Duration, error) {
	if n < 0 || unit < 0 || (n > 0 && int64(unit) > math.MaxInt64/int64(n)) {
		return 0, fmt.Errorf("%d times %v: %w", n, unit, ErrTooLong)
	}
	return time.Duration(n) * unit, nil
}
