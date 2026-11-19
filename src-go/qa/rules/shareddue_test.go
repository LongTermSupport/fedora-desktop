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

// Any package's limits that share a value are a shared due: a test naming that due by AwaitDue or
// Armed is flagged, in the package's own and its external tests, however the value is spelt; a
// due one limit alone has, a test's own constant and a timer named by its arming function are not.
func TestSharedDueAcrossLimits(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SharedDue, "fix/internal/sharedlimits")
}
