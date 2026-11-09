// Package badline is a Bubble Tea line that leaves on Esc but never reads an Esc joined to the
// next key, which Bubble Tea hands over as that key with Alt: the Esc is lost.
package badline

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

var (
	cancelKey = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("Esc", "cancels")) // want `Esc bound with no esckey.Joined check`
	otherKey  = key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("Ctrl-O", "opens"))
)

// Cancels is whether the press leaves the line.
func Cancels(k tea.KeyPressMsg) bool {
	return key.Matches(k, cancelKey) && !key.Matches(k, otherKey)
}
