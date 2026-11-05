package fakeclock_test

import (
	"testing"

	"fix/internal/fakeclock"
)

// Clean: the fake's external tests are its own package's too.
func TestOwnCountFromOutside(t *testing.T) {
	f := &fakeclock.Fake{}
	if n := f.Waiters(); n != 0 {
		t.Fatal(n)
	}
}
