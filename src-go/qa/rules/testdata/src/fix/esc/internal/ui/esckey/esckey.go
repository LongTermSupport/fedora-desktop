// Package esckey is a stand-in for internal/ui/esckey: whether a key press is an Esc joined to
// the next key.
package esckey

import tea "charm.land/bubbletea/v2"

// Joined is whether k is an Esc and the key after it read as one key with Alt.
func Joined(k tea.KeyPressMsg) bool { return k.Code != 0 }
