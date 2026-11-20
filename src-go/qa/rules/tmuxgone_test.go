package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// ccy-teams shutdown read a guest tmux server as empty when lxc-attach, runuser or env said
// "No such file or directory" (tmux never reached): the answer was matched loosely in five
// places of tmuxccy, and "can't find" in sessclose and winddown, beside the one classifier.
func TestTmuxGoneText(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TmuxGoneText,
		"fix/gone/internal/tmuxccy", "fix/gone/internal/badtmuxgone", "fix/internal/notmuxgone")
}
