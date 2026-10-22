// Package homefs is the one way a member opens its own files (SPEC-n3 TB4, GU1): a directory
// held open by fd as a root, and every path below it opened from that fd by openat2 with
// RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS, so nothing is reached through a
// link or from outside the root, even if a name above it is renamed meanwhile.
//
// A root and each directory it makes or opens must be the caller's (its effective uid): a
// member's teamchat directories mode 0700 (Private), a home or ~/.config only free of group and
// other write (Owned). Every leaf is the caller's too, and its mode is named by the caller and
// set by fchmod, whatever the umask. A file is placed by an exclusive create; a replace writes
// a fresh copy beside the old one, syncs it, renames it over the old one and syncs the
// directory, so a reader sees the old content or the new, never a part. A read opens without
// blocking, refuses anything but a regular file, and reads at most its cap.
//
// Paths are full paths, which must lie below the root's own path and be clean; an error names
// the path, never a file's content.
package homefs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrConfig is a path that is not as the member must have it: outside its root or unclean, not
// the caller's, the wrong mode or type, or over its cap (TB4, which makes it 78).
var ErrConfig = errors.New("homefs: not as the member must have it")

// Need is what a directory must be, beyond being the caller's and not a link.
type Need int

const (
	// Private is a member's own teamchat directory: mode 0700 exactly.
	Private Need = iota
	// Owned is a directory the member keeps its own above (a home, ~/.config): no group or
	// other write, so no one else can swap an entry in it.
	Owned
)

const (
	dirMode = 0o700
	// lockMode is a lock file's mode (B 2.5).
	lockMode = 0o600
	// sharedWrite is the group and other write bits an Owned directory must not have.
	sharedWrite = 0o022
	beneath     = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	// tempSuffix names the fresh copy a replace writes, hidden, before it renames it over the
	// old one: ".<name>.new".
	tempSuffix = ".new"
)

// Root is a directory held open by fd. Close it when done.
type Root struct {
	fd   int
	path string
	uid  int
}

// File is a regular file read whole: its content and the inode it was read from.
type File struct {
	Data []byte
	Dev  uint64
	Ino  uint64
	Perm fs.FileMode
}

// Entry is one name in a directory and its type (fs.ModeDir, fs.ModeSymlink, 0 for a regular
// file), as the directory listing gives it, never by following a link.
type Entry struct {
	Name string
	Type fs.FileMode
}

// Open opens path as a root: an absolute, clean directory that is not a link, the caller's,
// and as need says.
func Open(path string, need Need) (*Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: %s is not an absolute, clean path", ErrConfig, path)
	}
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathError(path, err)
	}
	r := &Root{fd: fd, path: path, uid: os.Geteuid()}
	if err := r.checkDir(fd, path, need); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return r, nil
}

// Path is the root's path.
func (r *Root) Path() string { return r.path }

// Close releases the root's fd.
func (r *Root) Close() error { return unix.Close(r.fd) }

// Sub opens the directory full, below the root or the root itself, as a root of its own: what
// is done through it stays in that directory even if it is renamed meanwhile.
func (r *Root) Sub(full string, need Need) (*Root, error) {
	fd, err := r.open(full, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := r.checkDir(fd, full, need); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return &Root{fd: fd, path: full, uid: r.uid}, nil
}

// MkdirAll makes each missing directory from the root down to full, mode 0700, syncing its
// parent; each one already there must be the caller's, mode 0700.
func (r *Root) MkdirAll(full string) error {
	rel, err := r.rel(full)
	if err != nil || rel == "." {
		return err
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		cur := r.path + "/" + strings.Join(parts[:i+1], "/")
		err := r.inParent(cur, func(parent int, name string) error {
			if err := mkdirAt(parent, name); err != nil && !errors.Is(err, unix.EEXIST) {
				return err
			}
			fd, err := unix.Openat2(parent, name, &unix.OpenHow{
				Flags: unix.O_DIRECTORY | unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: beneath,
			})
			if err != nil {
				return err
			}
			return errors.Join(r.checkDir(fd, cur, Private), unix.Close(fd))
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Mkdir makes full, mode 0700, refusing anything already there (fs.ErrExist), a link included.
func (r *Root) Mkdir(full string) error {
	return r.inParent(full, mkdirAt)
}

// mkdirAt makes name in parent with mode 0700 set by fchmod, and syncs parent.
func mkdirAt(parent int, name string) error {
	if err := unix.Mkdirat(parent, name, dirMode); err != nil {
		return err
	}
	fd, err := unix.Openat(parent, name, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := errors.Join(unix.Fchmod(fd, dirMode), unix.Close(fd)); err != nil {
		return err
	}
	return unix.Fsync(parent)
}

// Create writes a new file with mode perm, never over anything already there (fs.ErrExist),
// and syncs it and its directory.
func (r *Root) Create(full string, data []byte, perm fs.FileMode) error {
	return r.inParent(full, func(parent int, name string) error { return createAt(parent, name, data, perm) })
}

func createAt(parent int, name string, data []byte, perm fs.FileMode) error {
	fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, lockMode)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	werr := unix.Fchmod(fd, uint32(perm.Perm()))
	if werr == nil {
		_, werr = f.Write(data)
	}
	if werr == nil {
		werr = f.Sync()
	}
	if err := errors.Join(werr, f.Close()); err != nil {
		return err
	}
	return unix.Fsync(parent)
}

// Replace writes full afresh with mode perm: a new copy beside it, synced, renamed over it,
// the directory synced. Whatever was at full, a link included, is replaced, never followed.
func (r *Root) Replace(full string, data []byte, perm fs.FileMode) error {
	return r.inParent(full, func(parent int, name string) error {
		temp := "." + name + tempSuffix
		if err := unix.Unlinkat(parent, temp, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		if err := createAt(parent, temp, data, perm); err != nil {
			return err
		}
		if err := unix.Renameat(parent, temp, parent, name); err != nil {
			return errors.Join(err, unix.Unlinkat(parent, temp, 0))
		}
		return unix.Fsync(parent)
	})
}

// Read is the regular file full, the caller's, at most limit bytes; a missing file is
// fs.ErrNotExist. It opens without blocking, so a FIFO is refused, never waited on.
func (r *Root) Read(full string, limit int) (File, error) {
	fd, err := r.open(full, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return File{}, err
	}
	f := os.NewFile(uintptr(fd), full)
	got, err := r.readOpen(fd, f, full, limit)
	if err = errors.Join(err, f.Close()); err != nil {
		return File{}, err
	}
	return got, nil
}

// OpenLock opens the lock file full read-write (B 2.5), making it mode 0600 when it is not
// there; one already there must be the caller's regular file, mode 0600. The file is never
// truncated, unlinked or renamed here.
func (r *Root) OpenLock(full string) (*os.File, error) {
	const flags = unix.O_RDWR | unix.O_NONBLOCK
	fd, err := r.open(full, flags|unix.O_CREAT|unix.O_EXCL, lockMode)
	switch {
	case err == nil:
		err = r.madeLock(fd, full)
	case errors.Is(err, unix.EEXIST):
		fd, err = r.open(full, flags, 0)
	default:
		return nil, err
	}
	if err == nil {
		err = r.checkLock(fd, full)
	}
	if err != nil {
		if fd >= 0 {
			err = errors.Join(err, unix.Close(fd))
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), full), nil
}

// Entries are the names in the directory full, sorted, with their types.
func (r *Root) Entries(full string) ([]Entry, error) {
	fd, err := r.open(full, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return listFD(fd, full)
}

// listFD lists the directory fd and closes it.
func listFD(fd int, full string) ([]Entry, error) {
	f := os.NewFile(uintptr(fd), full)
	dirents, err := f.ReadDir(-1)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("homefs: list %s: %w", full, err)
	}
	entries := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		entries = append(entries, Entry{Name: d.Name(), Type: d.Type()})
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return entries, nil
}

// Exists is whether anything, a link included, is at full.
func (r *Root) Exists(full string) (bool, error) {
	found := false
	err := r.inParent(full, func(parent int, name string) error {
		var st unix.Stat_t
		err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW)
		found = err == nil
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	})
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	return found, err
}

// Unlink removes the file or link at full, never following it.
func (r *Root) Unlink(full string) error {
	return r.inParent(full, func(parent int, name string) error { return unix.Unlinkat(parent, name, 0) })
}

// Rename moves the entry at from to the name to, which must not exist (fs.ErrExist), and
// syncs both directories. Whatever is at from, a link included, moves unfollowed.
func (r *Root) Rename(from, to string) error {
	return r.inParent(from, func(fromParent int, fromName string) error {
		return r.inParent(to, func(toParent int, toName string) error {
			if err := unix.Renameat2(fromParent, fromName, toParent, toName, unix.RENAME_NOREPLACE); err != nil {
				return err
			}
			return errors.Join(unix.Fsync(fromParent), unix.Fsync(toParent))
		})
	})
}

// RemoveTree removes the directory full below the root and everything in it, following no
// link. The root itself is refused.
func (r *Root) RemoveTree(full string) error {
	if full == r.path {
		return fmt.Errorf("%w: will not remove the root %s", ErrConfig, full)
	}
	return r.inParent(full, func(parent int, name string) error {
		if err := removeAt(parent, name, full); err != nil {
			return err
		}
		return unix.Fsync(parent)
	})
}

// removeAt removes the directory name in parent with all it holds.
func removeAt(parent int, name, full string) error {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{
		Flags: unix.O_DIRECTORY | unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: beneath,
	})
	if err != nil {
		return err
	}
	dup, err := unix.Dup(fd)
	if err != nil {
		return errors.Join(err, unix.Close(fd))
	}
	entries, err := listFD(dup, full)
	for _, e := range entries {
		if err != nil {
			break
		}
		if e.Type.IsDir() {
			err = removeAt(fd, e.Name, full+"/"+e.Name)
		} else {
			err = unix.Unlinkat(fd, e.Name, 0)
		}
	}
	if err = errors.Join(err, unix.Close(fd)); err != nil {
		return err
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}

// rel is full's path relative to the root, "." for the root itself.
func (r *Root) rel(full string) (string, error) {
	if full == r.path {
		return ".", nil
	}
	rest, ok := strings.CutPrefix(full, r.path+"/")
	if !ok || filepath.Clean(full) != full {
		return "", fmt.Errorf("%w: %s is not a clean path below %s", ErrConfig, full, r.path)
	}
	return rest, nil
}

// open opens full below the root, through no link.
func (r *Root) open(full string, flags uint64, perm uint64) (int, error) {
	rel, err := r.rel(full)
	if err != nil {
		return -1, err
	}
	how := &unix.OpenHow{Flags: flags | unix.O_NOFOLLOW | unix.O_CLOEXEC, Mode: perm, Resolve: beneath}
	fd, err := unix.Openat2(r.fd, rel, how)
	if err != nil {
		return -1, pathError(full, err)
	}
	return fd, nil
}

// inParent runs f with the fd of full's directory and full's name in it.
func (r *Root) inParent(full string, f func(parent int, name string) error) (err error) {
	if _, relErr := r.rel(full); relErr != nil {
		return relErr
	}
	if full == r.path {
		return fmt.Errorf("%w: %s is the root, not a name in it", ErrConfig, full)
	}
	parent := filepath.Dir(full)
	fd := r.fd
	if parent != r.path {
		if fd, err = r.open(parent, unix.O_DIRECTORY|unix.O_RDONLY, 0); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, unix.Close(fd)) }()
	}
	if err := f(fd, filepath.Base(full)); err != nil {
		return pathError(full, err)
	}
	return nil
}

// checkDir is nil when fd is a directory the caller owns, as need says.
func (r *Root) checkDir(fd int, path string, need Need) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("homefs: %s: %w", path, err)
	}
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFDIR:
		return fmt.Errorf("%w: %s is not a directory", ErrConfig, path)
	case int(st.Uid) != r.uid:
		return fmt.Errorf("%w: %s is not this user's", ErrConfig, path)
	case need == Private && st.Mode&0o7777 != dirMode:
		return fmt.Errorf("%w: %s is not mode 0700", ErrConfig, path)
	case need == Owned && st.Mode&sharedWrite != 0:
		return fmt.Errorf("%w: %s is writable by its group or others", ErrConfig, path)
	}
	return nil
}

// madeLock gives a lock file just made its mode, whatever the umask, and syncs its directory.
func (r *Root) madeLock(fd int, full string) error {
	if err := unix.Fchmod(fd, lockMode); err != nil {
		return fmt.Errorf("homefs: %s: %w", full, err)
	}
	return r.inParent(full, func(parent int, _ string) error { return unix.Fsync(parent) })
}

// checkLock is nil when fd is the caller's regular file, mode 0600.
func (r *Root) checkLock(fd int, full string) error {
	st, err := r.regular(fd, full)
	if err == nil && st.Mode&0o7777 != lockMode {
		err = fmt.Errorf("%w: %s is not mode 0600", ErrConfig, full)
	}
	return err
}

func (r *Root) readOpen(fd int, f *os.File, full string, limit int) (File, error) {
	st, err := r.regular(fd, full)
	if err != nil {
		return File{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return File{}, fmt.Errorf("homefs: read %s: %w", full, err)
	}
	if len(data) > limit {
		return File{}, fmt.Errorf("%w: %s is over its cap of %d bytes", ErrConfig, full, limit)
	}
	return File{Data: data, Dev: st.Dev, Ino: st.Ino, Perm: fs.FileMode(st.Mode & 0o777)}, nil
}

// pathError wraps a failed call on full: a link, a name that is not a directory where one must
// be, and a path resolving outside the root are configuration faults (ErrConfig) as well as
// their errno.
func pathError(full string, err error) error {
	if !errors.Is(err, ErrConfig) && (errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EXDEV)) {
		return fmt.Errorf("%w: %s: %w", ErrConfig, full, err)
	}
	return fmt.Errorf("homefs: %s: %w", full, err)
}

// regular is fd's status when it is a regular file the caller owns.
func (r *Root) regular(fd int, full string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return st, fmt.Errorf("homefs: %s: %w", full, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return st, fmt.Errorf("%w: %s is not a regular file", ErrConfig, full)
	}
	if int(st.Uid) != r.uid {
		return st, fmt.Errorf("%w: %s is not this user's", ErrConfig, full)
	}
	return st, nil
}
