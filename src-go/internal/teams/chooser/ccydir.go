package chooser

import (
	"context"
	"errors"
	"fmt"
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
	if err := makeCcyDir(here.Top); err != nil {
		c.say("ccy-teams: " + err.Error() + ".")
		return exitConfig
	}
	return term.ExitOK
}

// ccyDirRel is the checkout's ccy directory, from its top.
const ccyDirRel = ".claude/ccy"

// makeCcyDir makes top's .claude/ccy/ (0700) and its .gitignore through an os.Root of the
// checkout, so a link a container put on the way (or at the .gitignore's name) is never followed
// out of it: the .gitignore is created exclusively, which never writes through a link. An
// existing real directory is left as it is; anything else at the name is refused.
func makeCcyDir(top string) (err error) {
	dir := filepath.Join(top, ccyDirRel)
	root, err := os.OpenRoot(top)
	if err != nil {
		return fmt.Errorf("could not open %s: %w", top, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	info, err := root.Lstat(ccyDirRel)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("%s is not a real directory; remove it and start cc again", dir)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s could not be read: %w", dir, err)
	}
	if err := root.MkdirAll(ccyDirRel, 0o700); err != nil {
		return fmt.Errorf("could not make %s: %w", dir, err)
	}
	f, err := root.OpenFile(ccyDirRel+"/.gitignore", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("could not write %s/.gitignore: %w", dir, err)
	}
	if _, err := f.WriteString(ccyDirIgnore); err != nil {
		return errors.Join(fmt.Errorf("could not write %s/.gitignore: %w", dir, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not write %s/.gitignore: %w", dir, err)
	}
	return nil
}
