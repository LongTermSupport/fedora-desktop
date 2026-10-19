package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// literal is a binding built as a struct literal: it can carry no help.
var literal = key.Binding{} // want `key.Binding with no help text`

// evade reads keys in the ways the reviewers found the rule could not see.
func evade(press tea.KeyPressMsg) string {
	k := press.String()
	if k == "ctrl+q" { // want `a key compared by hand`
		return "quit"
	}
	if k == "left" {
		return "editing"
	}
	switch s := press.String(); s {
	case "ctrl+o": // want `a key compared by hand`
		return "open"
	case "esc":
		return "leave"
	}
	if strings.HasPrefix(press.String(), "ctrl+") { // want `a key compared by hand`
		return "a control key"
	}
	if strings.Contains(k, "x") { // want `a key compared by hand`
		return "an x"
	}
	return ""
}
