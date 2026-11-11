package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A GitHub name a person, a flag or a play gave reaches teamchat's lower-case grammars only
// folded. The fixtures are the originating defect (a declaration's forge section handed to the
// strict canonical decoder), the second instance (a ping's reference written as the CLI gave it),
// the owner and OWNER/REPO validators called or taken as a value, a forge section built by hand,
// and beside them the folded forms, proto's own uses, a test file, and an allowance row that
// excuses one function and not its neighbour.
func TestIdentityFold(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "identityfold")
	allow := []allowance{
		{pkg: "internal/teamchat/member/state", fn: "validRef", reason: "the member's own file, written in the folded form"},
	}
	analysistest.Run(t, root, newIdentityFold(allow),
		"fix/internal/teamchat/proto", "fix/internal/teamchat/declare",
		"fix/internal/teamchat/member/engine", "fix/internal/teamchat/member/state")
}

func TestEveryIdentityFoldAllowanceHasAReason(t *testing.T) {
	for _, a := range identityFoldAllowances {
		if a.reason == "" || a.pkg == "" || a.fn == "" {
			t.Errorf("allowance %+v needs a package, a function and a reason", a)
		}
	}
}
