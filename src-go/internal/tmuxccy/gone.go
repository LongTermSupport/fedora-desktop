package tmuxccy

import (
	"errors"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// What a "not there" answer may be about: the server alone (a listing), or also the session
// (a command on one), or also its pane or window (a pane command).
type goneScope int

const (
	serverGone goneScope = iota
	sessionGone
	paneGone
)

// tmuxNotThereExit is the status tmux exits with when it says a server or target is not there.
const tmuxNotThereExit = 1

// goneAnswer is the one reading of tmux's "not there" answer (rule project_tmux_gone_text): exit
// 1, and every line of stderr one of tmux's own forms within scope. The same words from another
// program (lxc-attach, runuser or env, when the server is in an LXC container) are not tmux's
// answer: a line of theirs, or their exit code, makes it a failure.
func goneAnswer(err error, scope goneScope) bool {
	exitErr, ok := errors.AsType[*execx.ExitError](err)
	if !ok || exitErr.Code != tmuxNotThereExit || exitErr.Signal != 0 {
		return false
	}
	lines := 0
	for line := range strings.SplitSeq(strings.TrimSpace(exitErr.Stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines++
		noServer := strings.HasPrefix(line, "no server running on ") ||
			strings.HasPrefix(line, "error connecting to ") && strings.HasSuffix(line, " (No such file or directory)")
		noSession := scope >= sessionGone && strings.HasPrefix(line, "can't find session")
		noPane := scope >= paneGone && (strings.HasPrefix(line, "can't find pane") || strings.HasPrefix(line, "can't find window"))
		if !noServer && !noSession && !noPane {
			return false
		}
	}
	return lines > 0
}

// goneError is a command on a session that is not there: its message as the caller words it,
// matching ErrSessionGone under errors.Is.
type goneError struct{ msg string }

func (e *goneError) Error() string { return e.msg }

// Is matches ErrSessionGone.
func (e *goneError) Is(target error) bool { return target == ErrSessionGone }
