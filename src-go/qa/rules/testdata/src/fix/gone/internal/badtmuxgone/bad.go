// Package badtmuxgone holds tmux answers (it imports tmuxccy) and reads them by a loose match.
package badtmuxgone

import (
	"errors"
	"strings"

	"fix/gone/internal/tmuxccy"
)

// step is sessclose's check as it was: any error saying "can't find" taken as the session gone.
func step(err error) bool {
	return !strings.Contains(err.Error(), "can't find") // want `tmux's answer "can't find"`
}

// A wording kept in a constant is the same match.
const (
	noServer   = "no server running"                  // want `tmux's answer "no server running"`
	noSocket   = "error connecting to"                // want `tmux's answer "error connecting to"`
	noSuchFile = "x: No such file or directory here" // want `tmux's answer "No such file or directory"`
)

type answer struct {
	text string `json:"can't find"`
}

// gone asks the owner, which is clean; a comment may name "can't find session".
func gone(err error) bool { return errors.Is(err, errGone) && tmuxccy.Kill("") }

var errGone = errors.New("gone")

var _ = answer{}
