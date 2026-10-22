package host

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The modes of the directories root makes (C 3.2.2): its own 0700, and 0755 for the drop-in
// directory systemd reads and the status directory every account reads. Each is set by a chmod
// after the mkdir, so the umask of the shell that ran the verb does not decide it.
const (
	ownedDirMode  fs.FileMode = 0o700
	sharedDirMode fs.FileMode = 0o755
)

// mkdirOwned makes or converges a 0700 directory owned by uid and gid (-1 keeps the owner).
func mkdirOwned(dir string, uid, gid int) error {
	if err := mkdirMode(dir, ownedDirMode); err != nil {
		return err
	}
	return os.Lchown(dir, uid, gid)
}

// MkdirShared makes or converges a 0755 directory, whatever the umask; `team approve` makes the
// status directories with it.
func MkdirShared(dir string) error { return mkdirMode(dir, sharedDirMode) }

// MkdirStatus makes or converges the team's status directory and the `teams` one above it,
// under root: `team approve` makes them on a host no apply has run on yet.
func MkdirStatus(root string, team paths.TeamID) error {
	for _, dir := range []string{filepath.Dir(paths.StatusTeamDir(team)), paths.StatusTeamDir(team)} {
		if err := MkdirShared(root + dir); err != nil {
			return err
		}
	}
	return nil
}

// mkdirMode makes dir, or takes the directory that is there, and sets its mode exactly, so one a
// crash left before its chmod is converged; anything there that is not a directory (a link
// included) is an error.
func mkdirMode(dir string, mode fs.FileMode) error {
	if err := os.Mkdir(dir, ownedDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	ok, err := isDir(dir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return os.Chmod(dir, mode)
}

// isDir is whether path is a directory, not following a link; a missing path is not one.
func isDir(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// createOnly creates path with data and mode, never replacing a file that is there (link, not
// rename; H 7.03), and syncs the directory so the name outlives a power loss.
func createOnly(path string, data []byte, mode fs.FileMode) error {
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+rand.Text()+".tmp")
	f, err := os.OpenFile(filepath.Clean(temp), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	err = f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = os.Link(temp, path)
	}
	if err = errors.Join(err, os.Remove(temp)); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
