package durations

import (
	"fix/internal/duration"
	"strconv"
	"time"
)

const maxIntervalS = 86400

// interval is rclone-tail's shape: the number is refused above a constant before it is used.
func interval(s string) (time.Duration, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxIntervalS {
		return 0, err
	}
	return time.Duration(n) * time.Second, nil
}

// reversedBound is the same bound written the other way round.
func reversedBound(n int) time.Duration {
	if maxIntervalS < n {
		return 0
	}
	return time.Second * time.Duration(n)
}

// boundedFloat is a float refused above a constant.
func boundedFloat(f float64) time.Duration {
	if f > maxIntervalS {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

// graced is a field bounded where it is read, in another function of the package.
type graced struct{ grace int }

func (g *graced) read(n int) bool {
	g.grace = n
	if g.grace < 0 || g.grace > 30 {
		return false
	}
	return true
}

func (g *graced) wait() time.Duration {
	return time.Duration(g.grace) * time.Second
}

// offsetOfBounded subtracts a constant from a bounded number.
func offsetOfBounded(minutes int) time.Duration {
	if minutes > 10080 {
		return 0
	}
	return time.Duration(minutes-1) * time.Minute
}

// constants is a product of constants, which the compiler checks.
func constants() time.Duration {
	return time.Duration(7) * 24 * time.Hour
}

// durationTimesConstant is not built from a number: d is already a duration.
func durationTimesConstant(d time.Duration) time.Duration {
	return d * 2
}

// checked builds the product through the owner, which refuses an overflow.
func checked(minutes int, minute time.Duration) (time.Duration, error) {
	return duration.Times(minutes, minute)
}

// plainConversion converts nanoseconds without multiplying: int to int64 cannot overflow.
func plainConversion(ns int64) time.Duration {
	return time.Duration(ns)
}
