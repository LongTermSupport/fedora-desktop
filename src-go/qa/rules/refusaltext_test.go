package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A teamchat refusal's message says the cause and the next step, never the refusal's reason code
// alone: the code is already the error object's code. The fixtures are the ways a reason reaches
// a message (a fill of New, a fill built by Words or Format, a concatenation, a local copy, a
// value refusal) and the clean forms beside them (the reason as New's code, or as ExitOf's, a
// switch on it, a template chosen by it); a package outside teamchat completes the picture.
func TestRefusalReasonText(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "refusaltext")
	analysistest.Run(t, root, RefusalReasonText, "fix/internal/teamchat/badrefusal", "fix/internal/outsiderefusal")
}
