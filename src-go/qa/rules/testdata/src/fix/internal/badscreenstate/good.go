package badscreenstate

import "fix/internal/screen"

// taken names every state, so a new one is a question here and never a silent "taken".
func taken(pane string) bool {
	switch screen.ClaudeState(pane) {
	case screen.Ready, screen.Busy:
		return true
	case screen.Typed, screen.NotDrawn:
	}
	return false
}

// word compares a plain string, not a State.
func word(s string) bool { return s == "typed" }

// shown prints the state, which judges nothing.
func shown(pane string) string { return "box: " + string(screen.ClaudeState(pane)) }
