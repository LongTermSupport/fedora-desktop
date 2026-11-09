package badclip

import "testing"

// A test that pins the self-erasing copy holds the defect in place.
func TestCopyOnce(t *testing.T) {
	if copyOnce().Args[0] != "--paste-once" { // want `self-erasing clipboard`
		t.Fatal("args")
	}
}
