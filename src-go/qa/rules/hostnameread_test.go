package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The machine's hostname is read in one file of teamchat (declare/machinehost.go, for the refusal
// of a site that spells it), and a reverse lookup of an address is read nowhere. The fixtures are
// the ways a hostname is reached elsewhere (a call, a function value, an aliased or dot-imported
// os, the three spellings of LookupAddr, a test file) and the clean forms beside them (the
// environment, a forward lookup, a name of the package's own); the exempt file, a file of the same
// name in another package, a sibling file of the exempt one, a package outside teamchat and the
// commands complete the picture. They have a testdata root of their own, because the exemption is
// a file path.
func TestHostnameRead(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "hostnameread")
	analysistest.Run(t, root, HostnameRead,
		"fix/internal/teamchat/badhost", "fix/internal/teamchat/declare", "fix/internal/teamchat/other",
		"fix/internal/outsidehost", "fix/cmd/teamchat", "fix/cmd/teamchat-admin")
}
