package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A duration built from a number with no bound the rule can see is flagged: the originating
// prune cutoff, the other operand orders, a conversion held in a variable, an in-place
// multiply, a float conversion, a bound too large to keep the product inside int64, and a
// unit that is not a constant. A number bounded by a constant in its package, a constant
// product, a test file and the owner package that checks the product itself are clean.
func TestDurationOverflow(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), DurationOverflow,
		"fix/internal/durations", "fix/internal/duration")
}
