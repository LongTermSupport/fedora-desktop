package tmuxccy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// CurrentSession is the name of the CCY session the calling process runs in, from the TMUX
// variable it inherited from its pane, or "" when it runs in none (no tmux, or a tmux server
// other than ccy's), in which case tmux is not asked. tmux refusing to name the session is an
// error.
func (t *Tmux) CurrentSession(ctx context.Context, tmuxEnv string) (string, error) {
	if tmuxEnv == "" || filepath.Base(strings.Split(tmuxEnv, ",")[0]) != t.socket() {
		return "", nil
	}
	res, err := t.Run(ctx, "display-message", "-p", "#S")
	if err != nil {
		return "", fmt.Errorf("could not tell which ccy session this is: %s", reason(err))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}
