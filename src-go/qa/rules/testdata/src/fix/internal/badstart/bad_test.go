package badstart

import (
	"testing"

	"fix/internal/conversation"
	"fix/internal/registry"
)

// A test may build the arguments to compare with, and name the lookup's errors.
func TestBuilt(t *testing.T) {
	want, err := registry.FreshArgs("", "ccy", nil)
	if err != nil || len(want) == 0 || conversation.ErrNotRecorded == nil {
		t.Fatal("unexpected")
	}
}
