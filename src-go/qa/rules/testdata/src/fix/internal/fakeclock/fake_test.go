package fakeclock

import "testing"

// Clean: the fake's own tests assert its own count; nothing else arms a timer on it.
func TestOwnCount(t *testing.T) {
	f := &Fake{}
	if f.Waiters() != 0 {
		t.Fatalf("%d waiters", f.Waiters())
	}
}
