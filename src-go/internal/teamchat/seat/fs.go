package seat

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrConfig is a seat store, or a directory in it, that is not as the owner must have it: not
// a directory the caller owns with mode 0700, or reached through a link (SW2).
var ErrConfig = errors.New("seat: configuration")

const (
	dirMode  = 0o700
	fileMode = 0o600
	// beneath is how every path below a root is resolved: never above it, through no link.
	beneath = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	// replaceSuffix names the fresh copy a replace writes before it renames it over the old.
	replaceSuffix = ".new"
)

// dir is a root directory held open by fd: every path the store touches is opened from it
// (SW1-SW3), named by its full path, which must lie below the root.
type dir struct {
	fd   int
	path string
}

// openDir opens a root, making it 0700 first when create is set. It must be a directory the
// caller owns, mode 0700, and not a link.
func openDir(path string, create bool) (*dir, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: %s is not an absolute, clean path", ErrConfig, path)
	}
	if create {
		if err := os.MkdirAll(path, dirMode); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConfig, err)
		}
	}
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrConfig, path, err)
	}
	d := &dir{fd: fd, path: path}
	if err := ownedDir(fd, path); err != nil {
		return nil, errors.Join(err, d.close())
	}
	return d, nil
}

// ownedDir is nil when fd is a directory the caller owns, mode 0700.
func ownedDir(fd int, path string) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || int(st.Uid) != os.Geteuid() || st.Mode&0o7777 != dirMode {
		return fmt.Errorf("%w: %s is not a directory of this user with mode 0700", ErrConfig, path)
	}
	return nil
}

func (d *dir) close() error { return unix.Close(d.fd) }

// rel is full's path relative to the root.
func (d *dir) rel(full string) (string, error) {
	rest, ok := strings.CutPrefix(full, d.path+"/")
	if !ok || filepath.Clean(full) != full {
		return "", fmt.Errorf("%w: %s is not below %s", ErrConfig, full, d.path)
	}
	return rest, nil
}

// open opens full below the root, through no link.
func (d *dir) open(full string, flags uint64) (int, error) {
	rel, err := d.rel(full)
	if err != nil {
		return -1, err
	}
	how := &unix.OpenHow{Flags: flags | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: beneath}
	fd, err := unix.Openat2(d.fd, rel, how)
	if err != nil {
		return -1, fmt.Errorf("%s: %w", full, err)
	}
	return fd, nil
}

// inParent runs f with the fd of full's directory and full's name in it.
func (d *dir) inParent(full string, f func(parent int, name string) error) error {
	parent := filepath.Dir(full)
	fd := d.fd
	if parent != d.path {
		var err error
		if fd, err = d.open(parent, unix.O_DIRECTORY|unix.O_RDONLY); err != nil {
			return err
		}
		defer closeQuietly(fd)
	}
	if err := f(fd, filepath.Base(full)); err != nil {
		return fmt.Errorf("%s: %w", full, err)
	}
	return nil
}

// closeQuietly closes a read-only directory fd, whose close reports nothing a caller can act on.
func closeQuietly(fd int) {
	if err := unix.Close(fd); err != nil {
		return
	}
}

// create writes a new file, 0600, never over an existing one, and syncs it and its directory.
func (d *dir) create(full string, data []byte) error {
	return d.inParent(full, func(parent int, name string) error { return createAt(parent, name, data) })
}

func createAt(parent int, name string, data []byte) error {
	fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if err := errors.Join(werr, f.Close()); err != nil {
		return err
	}
	return unix.Fsync(parent)
}

// replace writes full afresh: a new copy beside it, renamed over it.
func (d *dir) replace(full string, data []byte) error {
	return d.inParent(full, func(parent int, name string) error {
		fresh := name + replaceSuffix
		if err := unix.Unlinkat(parent, fresh, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		if err := createAt(parent, fresh, data); err != nil {
			return err
		}
		if err := unix.Renameat(parent, fresh, parent, name); err != nil {
			return err
		}
		return unix.Fsync(parent)
	})
}

// read is a regular file's content, at most limit bytes.
func (d *dir) read(full string, limit int) ([]byte, error) {
	fd, err := d.open(full, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, err
	}
	return readFD(fd, full, limit)
}

// readFD reads a regular file from fd, at most limit bytes, and closes it.
func readFD(fd int, full string, limit int) ([]byte, error) {
	f := os.NewFile(uintptr(fd), full)
	data, err := readRegular(f, limit)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("%s: %w", full, err)
	}
	return data, nil
}

func readRegular(f *os.File, limit int) ([]byte, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, invalid("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, invalid("larger than its cap")
	}
	return data, nil
}

// exists is whether full names anything, a link included.
func (d *dir) exists(full string) (bool, error) {
	found := false
	err := d.inParent(full, func(parent int, name string) error {
		var st unix.Stat_t
		err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		found = err == nil
		return err
	})
	return found, err
}

// mkdirAll makes each missing directory from the root down to full, 0700, through no link.
func (d *dir) mkdirAll(full string) error {
	rel, err := d.rel(full)
	if err != nil {
		return err
	}
	cur := d.path
	for part := range strings.SplitSeq(rel, "/") {
		next := cur + "/" + part
		err := d.inParent(next, func(parent int, name string) error {
			if err := unix.Mkdirat(parent, name, dirMode); err != nil && !errors.Is(err, unix.EEXIST) {
				return err
			}
			fd, err := unix.Openat2(parent, name, &unix.OpenHow{
				Flags: unix.O_DIRECTORY | unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: beneath,
			})
			if err != nil {
				return err
			}
			closeQuietly(fd)
			return nil
		})
		if err != nil {
			return err
		}
		cur = next
	}
	return nil
}

// mkdirFresh makes full, 0700, refusing anything already there, a link included.
func (d *dir) mkdirFresh(full string) error {
	return d.inParent(full, func(parent int, name string) error {
		if err := unix.Mkdirat(parent, name, dirMode); err != nil {
			return err
		}
		return unix.Fsync(parent)
	})
}

// checkEmptyOwned is nil when full is an empty directory the caller owns, mode 0700.
func (d *dir) checkEmptyOwned(full string) error {
	fd, err := d.open(full, unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), full)
	return errors.Join(emptyOwned(f, fd, full), f.Close())
}

func emptyOwned(f *os.File, fd int, full string) error {
	if err := ownedDir(fd, full); err != nil {
		return err
	}
	names, err := f.Readdirnames(1)
	if len(names) > 0 {
		return fmt.Errorf("%w: %s is not empty", ErrConfig, full)
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", full, err)
	}
	return nil
}

// entries are the names in full, sorted.
func (d *dir) entries(full string) ([]string, error) {
	fd, err := d.open(full, unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), full)
	names, err := f.Readdirnames(-1)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("%s: %w", full, err)
	}
	slices.Sort(names)
	return names, nil
}

// unlink removes a file, or a link, never following it.
func (d *dir) unlink(full string) error {
	return d.inParent(full, func(parent int, name string) error { return unix.Unlinkat(parent, name, 0) })
}

// rmdir removes an empty directory.
func (d *dir) rmdir(full string) error {
	return d.inParent(full, func(parent int, name string) error {
		return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
	})
}

// rename renames the entry at from as the path given by renameat2 with flags, making its new
// directory first; whatever is at from, a link included, is moved without being followed.
func (d *dir) rename(from, to string, flags uint) error {
	if parent := filepath.Dir(to); parent != d.path {
		if err := d.mkdirAll(parent); err != nil {
			return err
		}
	}
	return d.inParent(from, func(fromParent int, fromName string) error {
		return d.inParent(to, func(toParent int, toName string) error {
			if err := unix.Renameat2(fromParent, fromName, toParent, toName, flags); err != nil {
				return err
			}
			return errors.Join(unix.Fsync(fromParent), unix.Fsync(toParent))
		})
	})
}
