package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A send's room is decided for every room by name: the originating defaults (a room that is not
// the reports room posted to main, a report tested for and every other send decoded as main's,
// a reports 403 tested before main's), in an if with an else, an if that returns, and a tagless
// switch with a later case, are flagged; a test for main, a side room's own addition, a last
// switch case and a switch over the room are not, and a package outside the rule's scope is
// not judged.
func TestRoomDefault(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), RoomDefault,
		"fix/internal/teamchat/member/engine", "fix/internal/teamchat/member/roomelsewhere")
}
