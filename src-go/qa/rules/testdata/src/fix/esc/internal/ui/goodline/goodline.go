// Package goodline is a Bubble Tea line that leaves on Esc, and on an Esc joined to the next
// key.
package goodline

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"fix/esc/internal/ui/esckey"
)

var cancelKey = key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancels"))

// Cancels is whether the press leaves the line.
func Cancels(k tea.KeyPressMsg) bool {
	return esckey.Joined(k) || key.Matches(k, cancelKey)
}
