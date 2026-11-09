package badlive

import "testing"

// A test may spell the words a fake tool prints: it shows nothing to a person.
func TestStates(t *testing.T) {
	if states([]member{{handle: "a"}})["a"] != "active" {
		t.Fatal("not active")
	}
}
