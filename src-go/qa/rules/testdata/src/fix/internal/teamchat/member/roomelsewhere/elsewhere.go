// Package roomelsewhere is outside the rule's scope: its tests of a room are not judged.
package roomelsewhere

import "fix/internal/teamchat/proto"

// isQuestions is a delivery's flag, not a send's room.
func isQuestions(room string) bool {
	if room != proto.PurposeQuestions {
		return false
	}
	return true
}
