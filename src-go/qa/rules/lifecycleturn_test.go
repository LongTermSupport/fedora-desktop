package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// The ends package's every lifecycle read and post is inside the team's turn: the originating
// race (back read and posted outside it), the ways round it (an alias, a method value, a
// helper chain, a goroutine started inside the turn), and the clean forms. The notice package
// and the host's callers are outside the rule's scope and carry no want.
func TestLifecycleTurn(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), LifecycleTurn,
		"fix/internal/teamchat/member/ends", "fix/internal/teamchat/notice", "fix/internal/teamchat/notice/elsewhere")
}
