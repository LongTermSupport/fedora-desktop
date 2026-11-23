// Package teamsync stands for teamchat's team goroutine: the start's check for a room to drop.
package teamsync

import (
	"slices"

	"qitem/internal/teamchat/member/state"
	"qitem/internal/teamchat/proto"
)

func holds(ps []state.Pending) bool {
	return slices.ContainsFunc(ps, func(p state.Pending) bool {
		return p.Room == proto.PurposeQuestions // want `a pending item's room tested against the questions room \(project_questions_item_by_room\)`
	})
}
