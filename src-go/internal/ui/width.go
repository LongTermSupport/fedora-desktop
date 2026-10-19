package ui

import "github.com/charmbracelet/x/ansi"

// Width is how many terminal cells text takes: a wide character counts two, a colour code none.
// Every column laid out for a terminal is measured with it.
func Width(text string) int { return ansi.StringWidth(text) }
