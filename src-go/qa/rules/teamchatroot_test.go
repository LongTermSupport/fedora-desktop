package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A teamchat machine record (a paths status-root name) handed to a call in ccy-teams with no
// TeamchatRoot in front is flagged: the status reader given the fixed path (the defect,
// system.StatusRecord), os reads of a record, a directory and a constant, a root after the path
// or a constant in front. The rooted forms (a field or a parameter named for the root), a name
// that is not a record, messages and a held path are not; nor is a test file or a package outside
// ccy-teams.
func TestTeamchatRootRead(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TeamchatRootRead,
		"teamchatroot/internal/teams/system", "teamchatroot/internal/elsewhere")
}
