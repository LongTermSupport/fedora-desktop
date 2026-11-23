// Package deliver stands for teamchat's delivery, which shows an item's room: out of the rule's
// scope.
package deliver

import (
	"qitem/internal/teamchat/member/state"
	"qitem/internal/teamchat/proto"
)

func inQuestions(p state.Pending) bool { return p.Room == proto.PurposeQuestions }
