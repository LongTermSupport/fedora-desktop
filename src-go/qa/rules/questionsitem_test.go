package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A pending item's room tested against the questions room in the engine and teamsync (the
// originating drop and its start check, by value, by pointer and either way round) is flagged;
// the item's own predicate, another type's room, main's room, package state and a package
// outside the rule's scope are not.
func TestQuestionsItem(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), QuestionsItem,
		"qitem/internal/teamchat/member/engine", "qitem/internal/teamchat/member/teamsync",
		"qitem/internal/teamchat/member/state", "qitem/internal/teamchat/member/deliver")
}
