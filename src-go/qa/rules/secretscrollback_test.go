package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A secret written to the terminal's normal screen stays in its scrollback (issue 255). The
// fixtures are the defect's shape (a password from agent-bus handed to a function that prints it
// in bold), the other ways a secret reaches a write (the kit's Tell and ReportErr, fmt to stderr
// and stdout, an os.File, an error carrying it, a field read in another method, a buffer, a
// wrapper returning it, a secret type's Reveal, a package's own printer wrapper, a password kept
// in a session field) and the clean forms beside them: the secret screen, a reader for a child's
// stdin, a constant-time compare, a length, a non-secret answer of the same client, a test file.
// They have a testdata root of their own, as the kit, the printer and the screen are stubs.
func TestSecretScrollback(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "secretscrollback")
	analysistest.Run(t, root, SecretScrollback,
		"fix/internal/agentbus", "fix/internal/term", "fix/internal/teams/flow",
		"fix/internal/teams/login", "fix/internal/ftpcamera", "fix/internal/ccc", "fix/internal/teamchat/admin")
}
