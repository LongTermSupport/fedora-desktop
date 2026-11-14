package badscreenstate

import (
	"testing"

	"fix/internal/screen"
)

// A test asserts one state by ==; tests are not judged.
func TestTyped(t *testing.T) {
	if screen.ClaudeState("❯ x") != screen.Typed {
		t.Fatal("not typed")
	}
}
