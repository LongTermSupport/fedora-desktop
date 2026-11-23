package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A seat-store ledger entry built field by field outside package state (the originating
// written-yes and proof sites, an element of a slice literal, a pointer, a positional literal
// and a field assigned on a declared entry) is flagged; the state constructor, an empty entry
// returned for "none", package state itself and a test file are not.
func TestLedgerBuilt(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), LedgerBuilt,
		"ledger/internal/teamchat/member/engine", "ledger/internal/teamchat/member/state")
}
