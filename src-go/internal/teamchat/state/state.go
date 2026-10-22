// Package state holds the host's state files the way C 3.6.1 requires (Plan 00209,
// DESIGN-v2-C-host.md 3.6.1, S15; SPEC-n2b.md ST1-ST2): a write is a temporary file beside the
// target, created exclusively without following a link, given its mode and group, written,
// synced and renamed over the target, then the directory synced; a read refuses a link, a file
// that is not regular, and a file over its cap, and a read of a file root decides from (ST3,
// SPEC-n2f's trust rule) also one of another owner or writable by its group or others.
package state

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// chown gives an open file its group; a variable so a test can make it fail as root.
var chown = func(f *os.File, gid int) error { return f.Chown(-1, gid) }

// WriteFile replaces path with data, mode and (unless gid is -1) group gid, so a reader sees
// either the old content or the new, never a part (ST1).
func WriteFile(path string, data []byte, mode fs.FileMode, gid int) error {
	dir := filepath.Dir(path)
	temp := filepath.Join(dir, "."+filepath.Base(path)+"."+rand.Text()+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", path, err)
	}
	if err := fill(f, data, mode, gid); err != nil {
		return errors.Join(fmt.Errorf("cannot write %s: %w", path, err), os.Remove(temp))
	}
	if err := os.Rename(temp, path); err != nil {
		return errors.Join(fmt.Errorf("cannot put %s in place: %w", path, err), os.Remove(temp))
	}
	return syncDir(dir)
}

// fill gives the temporary file its mode and group and its content, and closes it.
func fill(f *os.File, data []byte, mode fs.FileMode, gid int) error {
	err := f.Chmod(mode)
	if err == nil && gid != -1 {
		err = chown(f, gid)
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %s to sync it: %w", dir, err)
	}
	return errors.Join(d.Sync(), d.Close())
}

// readFlags open a file the reader decides from: never following a link, never blocking on a
// FIFO or a device, and (as every os.OpenFile) not inherited by a child.
const readFlags = os.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK

// ReadFile is path's content; found is false when there is no file. A link, a file that is not
// regular, and a file over limit bytes are refused (ST2).
func ReadFile(path string, limit int64) ([]byte, bool, error) {
	return read(path, limit, readFlags, nil)
}

// ReadOwnedFile is ReadFile of a file root decides from (ST3): it must also be owned by uid and
// writable by neither its group nor others.
func ReadOwnedFile(path string, limit int64, uid uint32) ([]byte, bool, error) {
	return read(path, limit, readFlags, &uid)
}

// ReadOwnedTarget is ReadOwnedFile for a name the distribution ships as a link (os-release):
// the link is followed, and the file it names is held to the same rules.
func ReadOwnedTarget(path string, limit int64, uid uint32) ([]byte, bool, error) {
	return read(path, limit, readFlags&^unix.O_NOFOLLOW, &uid)
}

func read(path string, limit int64, flags int, owner *uint32) ([]byte, bool, error) {
	f, err := os.OpenFile(path, flags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot open %s: %w", path, err)
	}
	data, err := readOpen(f, path, limit, owner)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	return data, err == nil, err
}

func readOpen(f *os.File, path string, limit int64, owner *uint32) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.Mode().IsRegular() || !ok:
		return nil, fmt.Errorf("%s is not a regular file", path)
	case owner != nil && st.Uid != *owner:
		return nil, fmt.Errorf("%s is owned by uid %d, not %d", path, st.Uid, *owner)
	case owner != nil && info.Mode().Perm()&0o022 != 0:
		return nil, fmt.Errorf("%s is writable by its group or by others", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is over %d bytes", path, limit)
	}
	return data, nil
}
