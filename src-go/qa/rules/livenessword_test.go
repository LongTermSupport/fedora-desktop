package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The ccy-teams board called every member that was not parked "active", which read as running:
// the word came from the registry's park state, not from the member's status on the bus.
func TestLivenessWord(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), LivenessWord,
		"fix/internal/badlive", "fix/internal/agentbus", "fix/internal/systemd", "fix/internal/stt", "fix/qa/rules")
}
