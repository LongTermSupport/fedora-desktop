package otherchild

import "testing"

type run struct{ Stderr string }

// A package whose tests never run their own binary: a child's empty stderr is not judged.
func TestQuiet(t *testing.T) {
	var r run
	if r.Stderr != "" {
		t.Fatal(r)
	}
}
