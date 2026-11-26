package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A seat-store add's refusal that reaches no return (the originating proof receipt's log line,
// the error held first, the result discarded, ErrFull matched and swallowed, a loss inside a
// function literal) is flagged; a refusal returned directly, wrapped or held first, an add that
// cannot refuse, package state itself and a test file are not.
func TestStoreRefusalLost(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), StoreRefusalLost,
		"storeadd/internal/teamchat/member/engine", "storeadd/internal/teamchat/member/state")
}
