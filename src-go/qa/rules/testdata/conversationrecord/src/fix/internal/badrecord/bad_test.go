package badrecord

import (
	"path/filepath"
	"testing"

	"fix/internal/conversation"
	"fix/internal/teams/membership"
)

// A test plants the record where the code reads it: clean.
func TestPlants(t *testing.T) {
	if filepath.Join(membership.SessionDir("/c", "s"), conversation.File, "conversation.json") == "" {
		t.Fatal("empty")
	}
}
