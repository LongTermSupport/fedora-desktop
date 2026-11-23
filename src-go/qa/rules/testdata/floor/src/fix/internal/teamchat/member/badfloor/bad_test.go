package badfloor

import "testing"

// A test may compare floors as numbers: it judges the values, not the events.
func TestFloorNumbers(t *testing.T) {
	floor := int64(5)
	if floor <= 4 {
		t.Fatal("floor")
	}
}
