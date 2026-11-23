// Package goodrecord reaches the record through the locator: clean.
package goodrecord

import (
	"fix/internal/conversation"
	"fix/internal/teams/membership"
)

// Through the locator, and other names beside the record's: clean.
func good(checkout, session string) []string {
	return []string{
		conversation.Locate(checkout, session, true),
		membership.SessionDir(checkout, session) + "/seat.json",
		"conversation.jsonl",
		"conversation",
	}
}

var _ = good
