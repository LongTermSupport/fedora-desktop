package chooser

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ccyDirIgnore is the .gitignore of a .claude/ccy/ a host launch makes: the seats' tree alone,
// which is all the seats need; ccy replaces it with its own full one the next time it runs
// there (it has no `*` rule).
const ccyDirIgnore = "# Created by cc --teams: the team seats hold tokens and are never committed.\npingbus/\n"

// hostReady makes this checkout's .claude/ccy/ (0700) for a host launch whose consent is given:
// `agent-bus seat take` and the seat keeper need it, as ccy makes it, so cc --teams works in a
// repository never used with ccy. An existing one is left as it is; one that is not a real
// directory is refused (78) and nothing is printed.
func (c *Chooser) hostReady(ctx context.Context) term.ExitCode {
	here, err := c.Host.Checkouts.Find(ctx, "")
	if err != nil {
		return c.failed(err)
	}
	if here.Top == "" {
		c.say("ccy-teams: cc --teams and cc --bus run in a git checkout: the seats live there.")
		return exitConfig
	}
	dir := filepath.Join(here.Top, ".claude", "ccy")
	info, err := os.Lstat(dir)
	switch {
	case err == nil && info.IsDir():
		return term.ExitOK
	case err == nil:
		c.say("ccy-teams: " + dir + " is not a real directory; remove it and start cc again.")
		return exitConfig
	case !errors.Is(err, fs.ErrNotExist):
		c.say("ccy-teams: " + dir + " could not be read: " + err.Error())
		return exitConfig
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.say("ccy-teams: could not make " + dir + ": " + err.Error())
		return exitConfig
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ccyDirIgnore), 0o600); err != nil {
		c.say("ccy-teams: could not write " + dir + "/.gitignore: " + err.Error())
		return exitConfig
	}
	return term.ExitOK
}
