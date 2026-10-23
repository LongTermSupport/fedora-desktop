package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A launch value is branched on only in package agent: the keeper's and the link's original
// comparisons, and the ways round them (a local copy of a constant, a named string constant, a
// switch on the field, a contains over the one list or a literal one, a string of an Agent, a
// map keyed by one, the field and a launch value given to one call, a map literal indexed by the
// field, the field through a call), are flagged, in teamchat's commands too; the agentBranches
// rows, the owner, the tests and a package outside teamchat are not, and a function beside a row is.
func TestAgentBranch(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), newAgentBranch(agentBranches),
		"fix/internal/teamchat/member/agent", "fix/internal/teamchat/member/state",
		"fix/internal/teamchat/member/wire", "fix/internal/teamchat/member/cli",
		"fix/internal/teamchat/member/launchuse", "fix/internal/elsewhereagent",
		"fix/cmd/teamchat", "fix/cmd/teamchat-admin")
}

func TestEveryAgentBranchHasAReason(t *testing.T) {
	for _, a := range agentBranches {
		if a.reason == "" || a.pkg == "" {
			t.Errorf("allowance %+v needs a package and a reason", a)
		}
	}
}
