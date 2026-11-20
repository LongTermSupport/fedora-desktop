// Package notmuxgone holds no tmux answer (it does not import tmuxccy): another program's
// "No such file or directory" is its own to read.
package notmuxgone

import "strings"

const noNmcli = `No such file or directory - Failed to exec "nmcli"`

// Missing is whether nmcli was missing in a container.
func Missing(stderr string) bool { return strings.Contains(stderr, noNmcli) }
