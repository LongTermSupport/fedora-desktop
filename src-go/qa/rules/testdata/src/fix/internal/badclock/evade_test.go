package badclock

import (
	"testing"
	"time"
)

// The evasions: a clock read that is not a call written out in a test function body.

var pkgNow = time.Now() // want `time.Now in a test`

var pkgClock = time.Now // want `time.Now in a test`

type fixture struct{ clock func() time.Time }

func stamp(clock func() time.Time) time.Time { return clock() }

func TestEvasions(t *testing.T) {
	n := time.Now // want `time.Now in a test`
	_ = n()
	_ = stamp(time.Now)          // want `time.Now in a test`
	_ = fixture{clock: time.Now} // want `time.Now in a test`
	since := time.Since          // want `time.Since in a test`
	_ = since
	_ = func() time.Duration { return time.Until(pkgNow) } // want `time.Until in a test`
}
