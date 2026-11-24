package badreader

import "testing"

// A test may compare against a selector.
func TestReads(t *testing.T) {
	if w := "--resume"; w != "--resume" {
		t.Fatal("unexpected")
	}
}
