package guestroot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// readChunk is the most one read(2) asks for.
	readChunk = 64 << 10
	// direntBuf is the buffer one getdents(2) fills.
	direntBuf = 8 << 10
)

// Stat is the inode at path by fstat of an O_PATH open: nothing is opened for reading, so a
// FIFO is only named. A link on the path is ErrRefused; a path that does not exist wraps
// fs.ErrNotExist.
func (r *Root) Stat(p string) (Info, error) {
	var info Info
	err := r.live(func() error {
		fd, i, err := r.opath(p)
		if err != nil {
			return err
		}
		info = i
		return r.closeFD(fd, p)
	})
	return info, err
}

// ReadFile is TB1's read of the regular file at path, at most limit bytes (over it is
// ErrTooLarge): data, the inode it was read from, and found false for a path that does not
// exist.
func (r *Root) ReadFile(p string, limit int64) ([]byte, Info, bool, error) {
	var (
		data  []byte
		info  Info
		found bool
	)
	err := r.live(func() error {
		fd, oerr := r.reopen(p, &info)
		if errors.Is(oerr, fs.ErrNotExist) {
			return nil
		}
		if oerr != nil {
			return oerr
		}
		var rerr error
		data, rerr = r.readAll(fd, p, limit)
		found = rerr == nil
		return errors.Join(rerr, r.closeFD(fd, p))
	})
	if err != nil || !found {
		return nil, Info{}, false, err
	}
	return data, info, true, nil
}

// ReadProcStat is /proc/<pid>/stat in the guest's own /proc, by ReadFile (cap 4 KiB): the one
// read that crosses into another mount below the root, which a read may (LX5). found is false
// for a pid with no process.
func (r *Root) ReadProcStat(pid int) ([]byte, bool, error) {
	if pid <= 0 {
		return nil, false, fmt.Errorf("%w: pid %d", ErrRefused, pid)
	}
	data, info, found, err := r.ReadFile("/proc/"+strconv.Itoa(pid)+"/stat", statCap)
	if err != nil || !found || !info.Mode.IsRegular() {
		return nil, false, err
	}
	return data, true, nil
}

// ReadDir is the names in the directory at path, sorted, opened by TB1's flags with
// O_DIRECTORY. Each name must pass valid, or the listing is refused naming the directory and
// never the entry; more than limit names is ErrTooLarge. A missing directory is found false.
func (r *Root) ReadDir(p string, valid func(string) bool, limit int) ([]string, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("guestroot: a listing of %s needs a positive limit", p)
	}
	var names []string
	found := false
	err := r.live(func() error {
		if perr := checkPath(p); perr != nil {
			return perr
		}
		fd, oerr := r.openBelow(r.fd, p, p, dirFlags, 0, readResolve)
		if errors.Is(oerr, fs.ErrNotExist) {
			return nil
		}
		if oerr != nil {
			return oerr
		}
		var lerr error
		names, lerr = r.entries(fd, p, limit)
		if lerr == nil && slices.ContainsFunc(names, func(n string) bool { return !valid(n) }) {
			lerr = fmt.Errorf("%w: an entry of %s fails its grammar", ErrRefused, p)
		}
		found = lerr == nil
		return errors.Join(lerr, r.closeFD(fd, p))
	})
	if err != nil || !found {
		return nil, false, err
	}
	return names, true, nil
}

// ProbeLock is the L3 probe's open (LP1): TB1's read open of the lock at path, then
// LOCK_SH|LOCK_NB on the reopened descriptor, taken and dropped at once (free) or EWOULDBLOCK
// (held), and the content read, at most 4 KiB, in both states. A refused open is ErrRefused,
// which the probe reads as free with no record; a missing lock wraps fs.ErrNotExist.
func (r *Root) ProbeLock(p string) (bool, []byte, error) {
	var (
		held   bool
		record []byte
	)
	err := r.live(func() error {
		var info Info
		fd, oerr := r.reopen(p, &info)
		if oerr != nil {
			return oerr
		}
		var lerr error
		held, lerr = r.tryShared(fd, p)
		if lerr == nil {
			record, lerr = r.readAll(fd, p, lockCap)
		}
		return errors.Join(lerr, r.closeFD(fd, p))
	})
	if err != nil {
		return false, nil, err
	}
	return held, record, nil
}

// tryShared is whether another description holds the lock on fd exclusively.
func (r *Root) tryShared(fd int, p string) (bool, error) {
	err := r.sys.flock(fd, unix.LOCK_SH|unix.LOCK_NB)
	switch {
	case errors.Is(err, unix.EWOULDBLOCK):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("guestroot: lock %s: %w", p, err)
	}
	if err := r.sys.flock(fd, unix.LOCK_UN); err != nil {
		return false, fmt.Errorf("guestroot: unlock %s: %w", p, err)
	}
	return false, nil
}

// DialSocket connects to the guest socket at path: opened O_PATH by TB1's flags, checked a
// socket owned by uid 0, then connected through /proc/self/fd/<n>, so no path is resolved in
// the guest. Anything else is ErrRefused, and nothing is connected.
func (r *Root) DialSocket(ctx context.Context, p string) (*net.UnixConn, error) {
	var conn *net.UnixConn
	err := r.live(func() error {
		fd, info, oerr := r.opath(p)
		if oerr != nil {
			return oerr
		}
		if info.Mode.Type() != fs.ModeSocket || info.UID != 0 {
			refused := fmt.Errorf("%w: %s is %s of uid %d, not root's socket", ErrRefused, p, kind(info.Mode), info.UID)
			return errors.Join(refused, r.closeFD(fd, p))
		}
		var derr error
		conn, derr = r.sys.dial(ctx, "/proc/self/fd/"+strconv.Itoa(fd))
		if derr != nil {
			derr = fmt.Errorf("guestroot: connect %s: %w", p, derr)
		}
		return errors.Join(derr, r.closeFD(fd, p))
	})
	if err != nil {
		if conn != nil {
			err = errors.Join(err, conn.Close())
		}
		return nil, err
	}
	return conn, nil
}

// checkPath refuses a path that is not absolute and clean, or holds a ".." or a NUL (n3-D14).
func checkPath(p string) error {
	if p == "" || p[0] != '/' || path.Clean(p) != p || strings.ContainsRune(p, 0) || slices.Contains(strings.Split(p, "/"), "..") {
		return fmt.Errorf("%w: %q is not a clean absolute path", ErrRefused, p)
	}
	return nil
}

// openBelow is the one openat2 of a name below dirfd, by TB1's resolve flags; full is the
// guest path an error names. An EAGAIN from a racing rename is tried again a bounded number
// of times.
func (r *Root) openBelow(dirfd int, name, full string, flags, mode, resolve uint64) (int, error) {
	how := unix.OpenHow{Flags: flags | unix.O_CLOEXEC, Mode: mode, Resolve: resolve}
	var err error
	for range openTries {
		var fd int
		fd, err = r.sys.openat2(dirfd, name, &how)
		if err == nil {
			return fd, nil
		}
		if !errors.Is(err, unix.EAGAIN) {
			break
		}
	}
	return -1, pathError(full, err)
}

// pathError is a failed open of full: a link, a name that is not a directory where one must
// be, and a mount crossed by a write are refusals as well as their errno.
func pathError(full string, err error) error {
	switch {
	case errors.Is(err, unix.ELOOP):
		return fmt.Errorf("%w: %s is or passes through a symbolic link: %w", ErrRefused, full, err)
	case errors.Is(err, unix.ENOTDIR):
		return fmt.Errorf("%w: %s passes through something that is not a directory: %w", ErrRefused, full, err)
	case errors.Is(err, unix.EXDEV):
		return fmt.Errorf("%w: %s crosses a mount: %w", ErrRefused, full, err)
	}
	return fmt.Errorf("guestroot: %s: %w", full, err)
}

// opath is path opened O_PATH below the root, and its inode.
func (r *Root) opath(p string) (int, Info, error) {
	if err := checkPath(p); err != nil {
		return -1, Info{}, err
	}
	fd, err := r.openBelow(r.fd, p, p, unix.O_PATH, 0, readResolve)
	if err != nil {
		return -1, Info{}, err
	}
	info, err := r.statFD(fd, p)
	if err != nil {
		return -1, Info{}, errors.Join(err, r.closeFD(fd, p))
	}
	return fd, info, nil
}

// reopen is TB1's read open of path: O_PATH, a regular file by fstat, then that inode reopened
// for reading through /proc/self/fd/<n> and checked to be the same by device and inode.
func (r *Root) reopen(p string, info *Info) (int, error) {
	pfd, i, err := r.opath(p)
	if err != nil {
		return -1, err
	}
	fd, err := r.reopenFD(pfd, i, p)
	if err = errors.Join(err, r.closeFD(pfd, p)); err != nil {
		if fd >= 0 {
			err = errors.Join(err, r.closeFD(fd, p))
		}
		return -1, err
	}
	*info = i
	return fd, nil
}

func (r *Root) reopenFD(pfd int, info Info, p string) (int, error) {
	if !info.Mode.IsRegular() {
		return -1, fmt.Errorf("%w: %s is %s, not a regular file", ErrRefused, p, kind(info.Mode))
	}
	fd, err := r.sys.openat(unix.AT_FDCWD, "/proc/self/fd/"+strconv.Itoa(pfd), reopenFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("guestroot: reopen %s: %w", p, err)
	}
	again, err := r.statFD(fd, p)
	if err == nil && (again.Dev != info.Dev || again.Ino != info.Ino) {
		err = fmt.Errorf("%w: %s changed while it was opened", ErrRefused, p)
	}
	if err != nil {
		return -1, errors.Join(err, r.closeFD(fd, p))
	}
	return fd, nil
}

// readAll reads fd to its end, at most limit bytes.
func (r *Root) readAll(fd int, p string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("guestroot: a read of %s needs a cap", p)
	}
	buf := make([]byte, min(limit+1, readChunk))
	var out []byte
	for int64(len(out)) <= limit {
		n, err := r.sys.read(fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("guestroot: read %s: %w", p, err)
		}
		if n == 0 {
			return out, nil
		}
		out = append(out, buf[:n]...)
	}
	return nil, fmt.Errorf("%w: %s is over its cap of %d bytes", ErrTooLarge, p, limit)
}

// entries is the names in the directory fd, "." and ".." left out, sorted; more than limit is
// ErrTooLarge.
func (r *Root) entries(fd int, p string, limit int) ([]string, error) {
	buf := make([]byte, direntBuf)
	var names []string
	for {
		n, err := r.sys.getdents(fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("guestroot: list %s: %w", p, err)
		}
		if n == 0 {
			break
		}
		consumed, added, all := unix.ParseDirent(buf[:n], -1, names)
		if consumed != n || added < 0 {
			return nil, fmt.Errorf("guestroot: list %s: a record cut short", p)
		}
		names = all
		if len(names) > limit {
			return nil, fmt.Errorf("%w: %s has more than %d entries", ErrTooLarge, p, limit)
		}
	}
	slices.Sort(names)
	return names, nil
}
