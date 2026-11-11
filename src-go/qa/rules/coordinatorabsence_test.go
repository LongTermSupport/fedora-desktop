package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A seat was told "the team has no coordinator" from a record not known to be current, and
// acted on it (issue 192). The fixtures are the absence clause written in each place the rule
// must tell from the one decided by ClaimCoordinator.
func TestCoordinatorAbsence(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), CoordinatorAbsence,
		"fix/internal/teamchat/badabsence", "fix/internal/teamchat/proto", "fix/internal/teamchat/member/hooktext",
		"fix/internal/unrelated")
}
