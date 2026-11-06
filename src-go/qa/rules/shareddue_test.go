package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A teamsync test that names the long-poll's due to the fake clock (AwaitDue, Armed) before it
// has settled the reports room is flagged, however the 30 s is spelt and in a closure too; a
// test that calls reportsSettled, a different due, an Advance alone and a package that does not
// declare reportsJoinLimit are not.
func TestSharedDue(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SharedDue,
		"fix/internal/teamchat/member/teamsync", "fix/internal/teamchat/member/noreports")
}
