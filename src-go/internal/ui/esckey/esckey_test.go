package esckey

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAnAltKeyIsAnEscJoinedToTheKey(t *testing.T) {
	joined := []tea.KeyPressMsg{
		{Code: tea.KeyDown, Mod: tea.ModAlt},
		{Code: tea.KeyEnter, Mod: tea.ModAlt},
		{Code: 'q', Text: "q", Mod: tea.ModAlt},
		{Code: 'r', Mod: tea.ModAlt | tea.ModCtrl},
		{Code: tea.KeyEscape, Mod: tea.ModAlt},
	}
	for _, k := range joined {
		if !Joined(k) {
			t.Errorf("%s is an Esc joined to the next key", k.String())
		}
	}
	plain := []tea.KeyPressMsg{
		{Code: tea.KeyDown},
		{Code: tea.KeyEscape},
		{Code: 'q', Text: "q"},
		{Code: 'r', Mod: tea.ModCtrl},
		{Code: 'Q', Text: "Q", Mod: tea.ModShift},
	}
	for _, k := range plain {
		if Joined(k) {
			t.Errorf("%s is not", k.String())
		}
	}
}
