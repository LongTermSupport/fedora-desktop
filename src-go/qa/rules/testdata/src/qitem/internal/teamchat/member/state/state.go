// Package state stands for teamchat's seat store: a pending item and its one predicate.
package state

import "qitem/internal/teamchat/proto"

// Pending is an item the store owes.
type Pending struct {
	Room      string
	RelayedBy string
}

// OfQuestionsSync is the one place the room is tested for a drop: not flagged here.
func (p Pending) OfQuestionsSync() bool {
	return p.Room == proto.PurposeQuestions && p.RelayedBy == ""
}

// Sending is a send in flight, whose room is not an item's.
type Sending struct{ Room string }
