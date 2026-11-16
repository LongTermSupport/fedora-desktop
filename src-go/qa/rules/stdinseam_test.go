package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Stdin is read only through internal/stdinread (issue #260). The fixtures are the reads the rule
// knows (the defect's io.ReadAll of a LimitReader over an env's Stdin first, then os.Stdin, a
// parameter, a pointer's field, a scanner, a decoder, a copy, a full read, a method read, a tee
// and a multi-reader), stdin handed on under another name (a reader parameter, a variable, a
// struct field), a command's main, and a test file that reads; and the clean forms beside them
// (stdin passed along as stdin, asked whether it is a terminal, compared with nil, given to a
// child, to the shared reader or to the ui seam, a string named Stdin, the file's descriptor).
// The seams (stdinread, ui, execx, term) and a test double read freely. They have a testdata
// root of their own, because the seams are matched by package path.
func TestStdinSeam(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "stdinseam")
	analysistest.Run(t, root, StdinSeam,
		"fix/internal/badstdin", "fix/internal/goodstdin", "fix/internal/stdinread", "fix/internal/ui",
		"fix/internal/execx", "fix/internal/term", "fix/internal/teams/teamtest", "fix/cmd/tool")
}
