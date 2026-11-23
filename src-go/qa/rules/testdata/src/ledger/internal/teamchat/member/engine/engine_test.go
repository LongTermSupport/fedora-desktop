package engine

import "ledger/internal/teamchat/member/state"

// A test builds its store's entries by hand, verb and all: not flagged.
var fixture = state.Ledger{ID: "$a", Item: "human", AtMS: 1}
