package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The delivery's item and recv's record of a human's reply in a question's thread were built
// with no question asked, while verify derived it on its own path (Plan 00265 R34). The fixtures
// are those two literals, a question decided by hand beside the resolver in each place it can be
// written, and the resolver's answer used everywhere, which is not flagged.
func TestQuestionUnresolved(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), QuestionUnresolved,
		"qres/internal/teamchat/member/engine", "qres/internal/teamchat/member/team")
}
