package allowedclock

import (
	"testing"
	"time"
)

func TestRealWait(t *testing.T) {
	start := time.Now()
	Wait()
	_ = time.Since(start)
}

func TestOtherWait(t *testing.T) {
	_ = time.Now() // want `time.Now in a test`
}
