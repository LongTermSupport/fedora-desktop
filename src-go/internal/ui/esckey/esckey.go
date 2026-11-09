// Package esckey tells a Bubble Tea key press that is an Esc joined to the next key. A
// terminal sends Esc as the byte that also starts every escape sequence, so Bubble Tea waits
// after it, and an Esc with the key after it in that wait or in one read (tmux, at its
// default escape-time, writes both at once) arrives as that key with Alt: Esc then Down is
// alt+down. A screen that cancels on Esc cancels on these too, or the Esc is lost
// (project_esc_joined, CLAUDE/GoProjectRules.md).
package esckey

import tea "charm.land/bubbletea/v2"

// Joined is whether k is an Esc and the key after it, read as that key with Alt.
func Joined(k tea.KeyPressMsg) bool {
	return k.Mod.Contains(tea.ModAlt)
}
