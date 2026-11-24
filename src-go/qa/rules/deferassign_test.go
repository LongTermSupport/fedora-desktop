package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A deferred function literal that assigns to a local of the enclosing function that is not a
// named result (the originating verifyDir form, an op-assignment, a nested literal's own local,
// a test file's) is flagged; a named result, a variable of the deferred literal itself, a field
// or pointer target and a deferred named function are not.
func TestDeferAssignLost(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), DeferAssignLost, "deferassign/a")
}
