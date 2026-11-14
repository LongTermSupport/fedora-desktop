// Package badscreenstate judges what the box shows by one state, lumping every other into the
// opposite verdict.
package badscreenstate

import "fix/internal/screen"

// boxEmptied is tmuxccy.Submit's check as it was (fedora-desktop#288): a held box, a redraw
// or a dialog is "taken".
func boxEmptied(pane string) bool {
	if screen.ClaudeState(pane) != screen.Typed { // want `a screen state judged by == or != puts every other state`
		return true
	}
	return false
}

// stillThere is the same verdict asked positively: its else is the lump.
func stillThere(pane string) bool {
	return screen.ClaudeState(pane) == screen.Typed // want `a screen state judged by == or != puts every other state`
}

// held is a state held in a variable, the constant on the left.
func held(pane string) bool {
	state := screen.ClaudeState(pane)
	return screen.Busy == state // want `a screen state judged by == or != puts every other state`
}

// tagless is a switch with no tag, whose cases are comparisons.
func tagless(pane string) string {
	state := screen.ClaudeState(pane)
	switch {
	case state == screen.Ready: // want `a screen state judged by == or != puts every other state`
		return "ready"
	default:
		return "other"
	}
}

// twoStates compares two read states with each other.
func twoStates(before, after string) bool {
	return screen.ClaudeState(before) == screen.ClaudeState(after) // want `a screen state judged by == or != puts every other state`
}
