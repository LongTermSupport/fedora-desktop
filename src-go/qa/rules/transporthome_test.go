package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A transport or TLS configuration is built in one home: package matrix (and pki, the
// certificate package, and tlsterm, the terminator, which builds the server's side). The fixtures are the ways one is built elsewhere in teamchat (a literal,
// a pointer or a value, new, a var, a Clone, an alias, a nested literal) and the clean forms
// beside them (a configuration only passed on, a pool, a listener); the owners, the test files,
// a package below matrix that is not matrix, a package outside teamchat and the two commands
// complete the picture. They have a testdata root of their own, because the owner is a package
// path and the shared root's matrix and command packages carry other rules' expectations.
func TestTransportHome(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "transporthome")
	analysistest.Run(t, root, TransportHome,
		"fix/internal/teamchat/badtls", "fix/internal/teamchat/matrix", "fix/internal/teamchat/matrix/below",
		"fix/internal/teamchat/pki", "fix/internal/teamchat/tlsterm", "fix/internal/outsidetls", "fix/cmd/teamchat",
		"fix/cmd/teamchat-admin")
}
