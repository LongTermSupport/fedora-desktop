package ui

import "testing"

func TestWidthIsTerminalCellsNotRunes(t *testing.T) {
	cases := map[string]int{
		"":                                     0,
		"proj.1+local.podman":                  19,
		"café":                                 4,
		string([]rune{0x65e5, 0x672c, 0x8a9e}): 6, // three wide (East Asian) characters
		"\x1b[1mbold\x1b[0m":                   4,
	}
	for text, want := range cases {
		if got := Width(text); got != want {
			t.Errorf("Width(%q) = %d, want %d", text, got, want)
		}
	}
}
