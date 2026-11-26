package engine

import "storeadd/internal/teamchat/member/state"

// A test fills its store past the cap on purpose: not flagged.
func overfill(s *state.Store) {
	s.AddReceipt(state.Receipt{ID: "$a"})
}
