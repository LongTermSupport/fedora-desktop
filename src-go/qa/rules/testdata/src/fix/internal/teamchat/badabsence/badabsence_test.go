package badabsence

import "testing"

// A test may spell the wording it expects.
func TestTheWording(t *testing.T) {
	if want := "the team has no coordinator now."; want == "" {
		t.Fatal(want)
	}
}
