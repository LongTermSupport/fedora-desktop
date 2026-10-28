package duration

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestTimesMultiplies(t *testing.T) {
	cases := []struct {
		n    int
		unit time.Duration
		want time.Duration
	}{
		{0, time.Hour, 0},
		{3, time.Minute, 3 * time.Minute},
		{1, math.MaxInt64, math.MaxInt64},
		{math.MaxInt64 / int(time.Second), time.Second, time.Duration(math.MaxInt64/int64(time.Second)) * time.Second},
	}
	for _, c := range cases {
		got, err := Times(c.n, c.unit)
		if err != nil || got != c.want {
			t.Errorf("Times(%d, %v) = %v, %v; want %v", c.n, c.unit, got, err, c.want)
		}
	}
}

// The originating defect: 106752 days of 24 hours wrapped to a negative duration.
func TestTimesRefusesWhatLeavesInt64(t *testing.T) {
	cases := []struct {
		n    int
		unit time.Duration
	}{
		{106752, 24 * time.Hour},
		{math.MaxInt64/int(time.Second) + 1, time.Second},
		{2, math.MaxInt64},
		{-1, time.Second},
		{1, -time.Second},
	}
	for _, c := range cases {
		got, err := Times(c.n, c.unit)
		if !errors.Is(err, ErrTooLong) || got != 0 {
			t.Errorf("Times(%d, %v) = %v, %v; want ErrTooLong", c.n, c.unit, got, err)
		}
	}
}
