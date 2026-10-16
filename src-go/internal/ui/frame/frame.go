// Package frame draws the frame every screen of the Go tools will share (Plan 00204,
// RESEARCH.md section 4.1). For now that is its first band, the breadcrumb title, and the
// smallest Bubble Tea program around it. It is the first user of the vendored Charm libraries
// (Bubble Tea, Bubbles, Lip Gloss); no command imports it yet.
package frame

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// crumbSeparator sits between two crumbs of the breadcrumb.
const crumbSeparator = " › "

// here is the style of the last crumb, where the person is: bold, in the one accent colour.
var here = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.Accent))

// quit is the program's one action. Its key is drawn in the footer, so it is never the only
// control surface (InteractiveScripts.md rule 14).
var quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))

// Title is the breadcrumb, the crumbs in order with the last one marked. No crumbs is no title.
func Title(crumbs []string) string {
	if len(crumbs) == 0 {
		return ""
	}
	last := len(crumbs) - 1
	return strings.Join(append(crumbs[:last:last], here.Render(crumbs[last])), crumbSeparator)
}

// Model is a screen of the frame: its title, and a footer drawn by Bubbles' help.
type Model struct {
	crumbs []string
	help   help.Model
}

// New is a screen whose breadcrumb is the crumbs.
func New(crumbs ...string) Model {
	return Model{crumbs: crumbs, help: help.New()}
}

// Init starts nothing.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update ends the program on the quit key and leaves the screen as it is on anything else.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if press, isKey := msg.(tea.KeyPressMsg); isKey && key.Matches(press, quit) {
		return m, tea.Quit
	}
	return m, nil
}

// View is the title, a blank line and the footer of actions.
func (m Model) View() tea.View {
	return tea.NewView(Title(m.crumbs) + "\n\n" + m.help.ShortHelpView([]key.Binding{quit}))
}
