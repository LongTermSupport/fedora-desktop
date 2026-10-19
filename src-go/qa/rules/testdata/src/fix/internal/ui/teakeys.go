package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Bindings with their words: ui draws the help line from them.
var (
	complete = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete"))
	quit     = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	bare     = key.NewBinding(key.WithKeys("ctrl+x"))                         // want `key.Binding with no help text`
	noWords  = key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "")) // want `key.Binding with no help text`
)

// helpOf names the words at run time, which the rule cannot read.
func helpOf(words string) key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", words)) // want `key.Binding whose help the rule cannot read`
}

// update reads keys, as only ui may: through bindings, and by hand only for line editing.
func update(msg tea.Msg) string {
	press, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return ""
	}
	switch {
	case key.Matches(press, complete), key.Matches(press, quit):
		return "bound"
	case press.Code == tea.KeyBackspace, press.String() == "left", press.Code == tea.KeyTab:
		return "editing"
	case press.String() == "ctrl+d": // want `a key compared by hand`
		return "delete everything"
	case press.Code == tea.KeyF1: // want `a key compared by hand`
		return "help"
	}
	switch press.Keystroke() {
	case "enter", "esc":
		return "line"
	case "ctrl+t": // want `a key compared by hand`
		return "take over"
	}
	return press.Text
}
