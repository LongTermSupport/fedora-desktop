// Package badtea is a flow that reads keys itself, outside the ui components that show them.
package badtea

import (
	tea "charm.land/bubbletea/v2"
)

// Model is a screen of its own.
type Model struct{}

// Update acts on a key nothing on screen names.
func (m Model) Update(msg tea.Msg) Model {
	switch msg := msg.(type) {
	case tea.KeyPressMsg: // want `Bubble Tea key message read outside internal/ui`
		if msg.String() == "ctrl+x" { // want `a key compared by hand`
			return Model{}
		}
	}
	if _, isKey := msg.(tea.KeyMsg); isKey { // want `Bubble Tea key message read outside internal/ui`
		return m
	}
	return m
}

// released takes a key message as a parameter.
func released(k tea.KeyReleaseMsg) bool { // want `Bubble Tea key message read outside internal/ui`
	return k.Text != ""
}
