package durations

import (
	"testing"
	"time"
)

// A test's numbers are its own, written in the file: not judged.
func TestUnjudged(t *testing.T) {
	n := 3
	if time.Duration(n)*time.Second <= 0 {
		t.Fatal("not positive")
	}
}
