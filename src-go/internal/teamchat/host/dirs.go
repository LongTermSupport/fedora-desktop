package host

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The modes of the directories root makes (C 3.2.2): its own 0700, and 0755 for the drop-in
// directory systemd reads and the status directory every account reads. Each is set by a chmod
// after the mkdir, so the umask of the shell that ran the verb does not decide it.
const (
	ownedDirMode  fs.FileMode = 0o700
	sharedDirMode fs.FileMode = 0o755
)

// mkdirOwned makes a 0700 directory owned by uid and gid (-1 keeps the creator's), or accepts
// one that is there and is a directory.
func mkdirOwned(dir string, uid, gid int) error {
	made, err := mkdirMode(dir, ownedDirMode)
	if err != nil || !made {
		return err
	}
	return os.Lchown(dir, uid, gid)
}

// MkdirShared makes a 0755 directory, whatever the umask, or accepts one that is there and is a
// directory; `team approve` makes the team's status directory with it.
func MkdirShared(dir string) error {
	_, err := mkdirMode(dir, sharedDirMode)
	return err
}

// mkdirMode makes dir with mode exactly and answers true, or answers false for a directory
// that is there; anything else there is an error.
func mkdirMode(dir string, mode fs.FileMode) (bool, error) {
	if err := os.Mkdir(dir, ownedDirMode); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return false, err
		}
		ok, derr := isDir(dir)
		if derr == nil && !ok {
			derr = fmt.Errorf("%s is not a directory", dir)
		}
		return false, derr
	}
	return true, os.Chmod(dir, mode)
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
// rename; H 7.03).
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
	return errors.Join(err, os.Remove(temp))
}
