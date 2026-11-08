// Package state holds the host's state files the way C 3.6.1 requires (Plan 00209,
// DESIGN-v2-C-host.md 3.6.1, S15; SPEC-n2b.md ST1-ST2): a write is a temporary file beside the
// target, created exclusively without following a link, given its mode and group, written,
// synced and renamed over the target, then the directory synced; a read refuses a link, a file
// that is not regular, and a file over its cap, and a read of a file root decides from (ST3,
// SPEC-n2f's trust rule) also one of another owner or writable by its group or others, and a read
// of a private key's file (ST4, Plan 00246 X13) also one whose mode is not exactly 0600.
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
	dir, name := filepath.Split(path)
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", path, err)
	}
	return errors.Join(writeIn(root, path, name, data, mode, gid), root.Close())
}

// writeIn does WriteFile's work through root, the directory of path: the temporary file, the
// rename and the sync are all made in that one directory, and a root creating a file exclusively
// never follows a link.
func writeIn(root *os.Root, path, name string, data []byte, mode fs.FileMode, gid int) error {
	temp := "." + name + "." + rand.Text() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", path, err)
	}
	if err := fill(f, data, mode, gid); err != nil {
		return errors.Join(fmt.Errorf("cannot write %s: %w", path, err), root.Remove(temp))
	}
	if err := root.Rename(temp, name); err != nil {
		return errors.Join(fmt.Errorf("cannot put %s in place: %w", path, err), root.Remove(temp))
	}
	return syncDir(root, filepath.Dir(path))
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

func syncDir(root *os.Root, dir string) error {
	d, err := root.Open(".")
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
	return read(path, limit, readFlags, trust{})
}

// ReadOwnedFile is ReadFile of a file root decides from (ST3): it must also be owned by uid and
// writable by neither its group nor others.
func ReadOwnedFile(path string, limit int64, uid uint32) ([]byte, bool, error) {
	return read(path, limit, readFlags, trust{owner: &uid})
}

// ReadOwnedTarget is ReadOwnedFile for a name the distribution ships as a link (os-release):
// the link is followed, and the file it names is held to the same rules.
func ReadOwnedTarget(path string, limit int64, uid uint32) ([]byte, bool, error) {
	return read(path, limit, readFlags&^unix.O_NOFOLLOW, trust{owner: &uid})
}

// privateMode is the one mode a private key's file may have: its owner's alone.
const privateMode fs.FileMode = 0o600

// modeBits are the bits of a file's mode a private mode is compared on: the permissions and
// the setuid, setgid and sticky bits.
const modeBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// ReadPrivateFile is ReadOwnedFile of a private key's file (ST4): its mode must also be 0600
// exactly. The owner and the mode are judged on the file opened, never on a name checked first.
// A refusal for the owner is an *OwnerError, one for the mode a *ModeError.
func ReadPrivateFile(path string, limit int64, uid uint32) ([]byte, bool, error) {
	return read(path, limit, readFlags, trust{owner: &uid, private: true})
}

// OwnerError is a file refused because uid UID owns it, not Want.
type OwnerError struct {
	Path      string
	UID, Want uint32
}

func (e *OwnerError) Error() string {
	return fmt.Sprintf("%s is owned by uid %d, not %d", e.Path, e.UID, e.Want)
}

// ModeError is a private file refused because its mode is Mode, not 0600.
type ModeError struct {
	Path string
	Mode fs.FileMode
}

func (e *ModeError) Error() string {
	return fmt.Sprintf("%s has mode %04o, not %04o", e.Path, uint32(e.Mode), uint32(privateMode))
}

// trust is what a read requires of the file beyond ST2: an owner (ST3), and a private mode (ST4).
type trust struct {
	owner   *uint32
	private bool
}

// openIn opens the last element of path, relative to a handle on its directory: the element is
// one name that cannot lead out of that directory, and with unix.O_NOFOLLOW a link in its place
// is refused by the kernel in the same call, not by a check made before it. os.Root cannot
// stand in for this: whatever flags it is given, it follows a link that stays inside its root,
// and ST2 refuses one. The handle is O_PATH, so a directory the caller may search but not list
// still opens its files, as it did when the whole path was opened at once.
func openIn(path string, flags int) (*os.File, error) {
	dir, name := filepath.Split(path)
	if name == "" {
		name = "."
	}
	dirFD, err := unix.Open(filepath.Clean(dir), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	fd, openErr := unix.Openat(dirFD, name, flags|unix.O_CLOEXEC, 0)
	closeErr := unix.Close(dirFD)
	if openErr == nil && closeErr != nil {
		closeErr = errors.Join(closeErr, unix.Close(fd))
	}
	if err = errors.Join(openErr, closeErr); err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func read(path string, limit int64, flags int, want trust) ([]byte, bool, error) {
	f, err := openIn(path, flags)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot open %s: %w", path, err)
	}
	data, err := readOpen(f, path, limit, want)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	return data, err == nil, err
}

func readOpen(f *os.File, path string, limit int64, want trust) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	owner := want.owner
	switch {
	case !info.Mode().IsRegular() || !ok:
		return nil, fmt.Errorf("%s is not a regular file", path)
	case owner != nil && st.Uid != *owner:
		return nil, &OwnerError{Path: path, UID: st.Uid, Want: *owner}
	case want.private && info.Mode()&modeBits != privateMode:
		return nil, &ModeError{Path: path, Mode: info.Mode() & modeBits}
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
