package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A screen.State judged by == or != puts every other state, one the reader does not know
// included, in the opposite verdict (fedora-desktop#288): the fixture has Submit's "anything
// not typed is taken", a positive test whose else is the verdict, a comparison inside package
// screen, a tagless switch and a value held in a variable; an exhaustive switch, a string
// compared as a string and a test file are not judged.
func TestScreenStateCompared(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ScreenStateCompared, "fix/internal/screen", "fix/internal/badscreenstate")
}
