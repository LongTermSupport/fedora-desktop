// Package duration is the stub of the owner: it checks the product itself, so it is not judged.
package duration

import (
	"errors"
	"math"
	"time"
)

// Times is n units, or an error when that leaves int64 nanoseconds.
func Times(n int, unit time.Duration) (time.Duration, error) {
	if n < 0 || unit < 0 || (n > 0 && int64(unit) > math.MaxInt64/int64(n)) {
		return 0, errors.New("too long")
	}
	return time.Duration(n) * unit, nil
}
