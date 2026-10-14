package badclock

import (
	"testing"
	"time"
)

// A date-dependent test failed in the hour after midnight.
func TestWallClock(t *testing.T) {
	now := time.Now()   // want `time.Now in a test`
	_ = time.Since(now) // want `time.Since in a test`
	_ = time.Until(now) // want `time.Until in a test`
}

func TestFixed(t *testing.T) {
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	_ = at.Add(time.Hour)
}
