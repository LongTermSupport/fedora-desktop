// Package frame is a ui component below internal/ui: it may read keys.
package frame

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

var quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))

// Update ends on the quit key.
func Update(msg tea.Msg) bool {
	press, isKey := msg.(tea.KeyPressMsg)
	return isKey && key.Matches(press, quit)
}
