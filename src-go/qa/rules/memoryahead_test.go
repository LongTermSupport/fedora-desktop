package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Engine memory written inside a store write's change function (recv's settle forgetting
// content, a field of the engine or the held team set, a counter raised, a map cleared, a
// helper reached through two calls) is flagged; the store copy and the closure's own
// variables, memory changed after the write, the log and the failpoints, a test file and
// another package are not.
func TestMemoryAheadOfStore(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), MemoryAheadOfStore,
		"memahead/internal/teamchat/member/engine", "memahead/internal/teamchat/member/other")
}
