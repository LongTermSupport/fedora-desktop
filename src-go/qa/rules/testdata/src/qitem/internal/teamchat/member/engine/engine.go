// Package engine stands for teamchat's engine: the demotion's drop.
package engine

import (
	"slices"

	"qitem/internal/teamchat/member/state"
	"qitem/internal/teamchat/proto"
)

// drop is the originating drop: a relayed answer goes with the room's own items.
func drop(ps []state.Pending) []state.Pending {
	return slices.DeleteFunc(ps, func(p state.Pending) bool {
		return p.Room == proto.PurposeQuestions // want `a pending item's room tested against the questions room \(project_questions_item_by_room\)`
	})
}

// byPointer and notQuestions are the same test, reversed and negated.
func byPointer(p *state.Pending) bool {
	return proto.PurposeQuestions == p.Room // want `a pending item's room tested against the questions room \(project_questions_item_by_room\)`
}

func notQuestions(p state.Pending) bool {
	return p.Room != proto.PurposeQuestions // want `a pending item's room tested against the questions room \(project_questions_item_by_room\)`
}

// kept uses the item's own predicate.
func kept(ps []state.Pending) []state.Pending {
	return slices.DeleteFunc(ps, state.Pending.OfQuestionsSync)
}

// sends tests a send's room, and main's room is tested for an item: neither is flagged.
func sends(xs []state.Sending, p state.Pending) bool {
	return slices.ContainsFunc(xs, func(x state.Sending) bool { return x.Room == proto.PurposeQuestions }) || p.Room == proto.PurposeMain
}
