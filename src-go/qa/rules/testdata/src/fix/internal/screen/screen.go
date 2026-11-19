// Package screen stands in for internal/screen: what Claude's input box shows.
package screen

import "strings"

// State is what Claude's input box shows.
type State string

// The states of Claude's prompt.
const (
	Ready    State = "ready"
	Busy     State = "busy"
	Typed    State = "typed"
	NotDrawn State = "not-drawn"
)

// ClaudeState reads the box.
func ClaudeState(pane string) State {
	if strings.Contains(pane, "❯ x") {
		return Typed
	}
	return Ready
}

// AtPicker is the picker check as it was: a comparison inside package screen is judged too.
func AtPicker(pane string) bool {
	if ClaudeState(pane) != NotDrawn { // want `a screen state judged by == or != puts every other state`
		return false
	}
	return strings.Contains(pane, "Resume session")
}

// Drawn asks every state by name, so a new one is a compile-time question here.
func Drawn(pane string) bool {
	switch ClaudeState(pane) {
	case Ready, Busy, Typed:
		return true
	case NotDrawn:
	}
	return false
}
