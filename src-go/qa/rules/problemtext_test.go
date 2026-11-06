package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A status problem's text is a fixed class, never an error's own text: the record refuses a
// problem over 512 bytes or with a control character, and the whole team's record is then not
// written. The fixtures are the ways an error's text reaches a problem (Error() in What or Fix,
// a concatenation, an error formatted by fmt, NewProblem, a pointer literal, a test file) and the
// clean forms beside them (a sentinel's Error(), a class function, a string of the package's own);
// a package outside teamchat completes the picture.
func TestProblemErrorText(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "problemtext")
	analysistest.Run(t, root, ProblemErrorText, "fix/internal/teamchat/badproblem", "fix/internal/outsideproblem")
}
