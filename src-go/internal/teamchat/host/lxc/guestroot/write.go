package guestroot

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// tempPrefix names what a write makes before it renames it in: ".new-<name>". A name a
	// caller passes may not start with it, so a temporary is never a digest or a unit.
	tempPrefix = ".new-"
	// dirMode is every directory a write makes (C 3.9.1).
	dirMode = 0o755
	// maxTreeDepth bounds RemoveTree's descent.
	maxTreeDepth = 16
	// maxTreeEntries bounds one directory RemoveTree empties.
	maxTreeEntries = 4096
	// linkMax is the longest link target readlinkat reads.
	linkMax  = 4096
	dirFlags = unix.O_DIRECTORY | unix.O_RDONLY | unix.O_NOFOLLOW
)

// Writer is the root-write rules (C 3.9.1): every open adds RESOLVE_NO_XDEV, and every
// directory on the path is opened O_DIRECTORY|O_NOFOLLOW and checked by fstat root-owned and
// not group or other writable before anything is made in it. What a write makes is root's,
// with its mode set by fchmod whatever the umask.
type Writer struct{ r *Root }

// Writer is the root's writer.
func (r *Root) Writer() *Writer { return &Writer{r: r} }

// MkdirAll makes each missing directory of path, 0755 root, and checks each one already there
// by the rules; it says whether one was made.
func (w *Writer) MkdirAll(p string) (bool, error) {
	changed := false
	err := w.r.live(func() error {
		fd, made, werr := w.walk(p, true)
		if werr != nil {
			return werr
		}
		changed = made
		return w.r.closeFD(fd, p)
	})
	return changed, err
}

// WriteFile places data at dir/name with mode by an O_CREAT|O_EXCL|O_NOFOLLOW temporary,
// fchown root, fchmod mode, fsync, renameat2 over the old name and a directory fsync; the same
// bytes and mode already there, root's, are left alone (changed false).
func (w *Writer) WriteFile(dir, name string, data []byte, mode fs.FileMode) (bool, error) {
	if err := errors.Join(checkName(name), checkMode(mode)); err != nil {
		return false, err
	}
	changed := false
	err := w.inDir(dir, func(dfd int) error {
		var werr error
		changed, werr = w.writeIn(dfd, join(dir, name), name, data, mode)
		return werr
	})
	return changed, err
}

// PlaceDir fills a fresh temporary directory under parent with fill, then renames it to name
// with RENAME_NOREPLACE. A directory already at name is left as it is (changed false) once it
// passes the rules; anything else there is refused. A failed fill removes the temporary.
func (w *Writer) PlaceDir(parent, name string, fill func(d *Dir) error) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	changed := false
	err := w.inDir(parent, func(pfd int) error {
		var perr error
		changed, perr = w.placeIn(pfd, parent, name, fill)
		return perr
	})
	return changed, err
}

// Symlink makes dir/name a link to target by symlinkat to a temporary and renameat2 over the
// old name; a link already naming target is left alone (changed false). The old name is read
// by readlinkat, never followed.
func (w *Writer) Symlink(dir, name, target string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	if target == "" || strings.ContainsRune(target, 0) {
		return false, fmt.Errorf("%w: %q is not a link target", ErrRefused, target)
	}
	changed := false
	err := w.inDir(dir, func(dfd int) error {
		var lerr error
		changed, lerr = w.linkIn(dfd, join(dir, name), name, target)
		return lerr
	})
	return changed, err
}

// Remove unlinks dir/name, a file or a link, never a directory; absent is removed false.
func (w *Writer) Remove(dir, name string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	full := join(dir, name)
	removed := false
	err := w.inDir(dir, func(dfd int) error {
		uerr := w.r.sys.unlinkat(dfd, name, 0)
		switch {
		case errors.Is(uerr, unix.ENOENT):
			return nil
		case errors.Is(uerr, unix.EISDIR):
			return fmt.Errorf("%w: %s is a directory", ErrRefused, full)
		case uerr != nil:
			return fmt.Errorf("guestroot: remove %s: %w", full, uerr)
		}
		removed = true
		return w.sync(dfd, dir)
	})
	return removed, err
}

// RemoveTree removes the directory dir/name and everything under it through directory fds,
// following no link (an old kit digest); absent is removed false.
func (w *Writer) RemoveTree(dir, name string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	removed := false
	err := w.inDir(dir, func(dfd int) error {
		var rerr error
		removed, rerr = w.removeIn(dfd, join(dir, name), name, 0)
		if rerr != nil || !removed {
			return rerr
		}
		return w.sync(dfd, dir)
	})
	return removed, err
}

// Dir is a directory being filled by PlaceDir: Mkdir and WriteFile relative to it, each
// mode explicit and root's.
type Dir struct {
	w    *Writer
	fd   int
	full string
	// subs is every directory Mkdir opened in this placement, closed when it ends.
	subs *[]int
}

// Mkdir makes the directory name in d, 0755 root.
func (d *Dir) Mkdir(name string) (*Dir, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	full := join(d.full, name)
	fd, err := d.w.makeDir(d.fd, name, full)
	if err != nil {
		return nil, err
	}
	*d.subs = append(*d.subs, fd)
	return &Dir{w: d.w, fd: fd, full: full, subs: d.subs}, nil
}

// WriteFile writes the new file name in d with mode, root's.
func (d *Dir) WriteFile(name string, data []byte, mode fs.FileMode) error {
	if err := errors.Join(checkName(name), checkMode(mode)); err != nil {
		return err
	}
	return d.w.createIn(d.fd, name, join(d.full, name), data, mode)
}

// inDir runs f with dir opened by the rules, on a live root.
func (w *Writer) inDir(dir string, f func(dfd int) error) error {
	return w.r.live(func() error {
		dfd, _, err := w.walk(dir, false)
		if err != nil {
			return err
		}
		return errors.Join(f(dfd), w.r.closeFD(dfd, dir))
	})
}

// walk opens each directory of path from the root down by the rules, making a missing one
// when mk is set: the last one's descriptor, and whether one was made.
func (w *Writer) walk(p string, mk bool) (int, bool, error) {
	if err := checkPath(p); err != nil {
		return -1, false, err
	}
	fd, err := w.step(w.r.fd, "/", "/")
	if err != nil || p == "/" {
		return fd, false, err
	}
	made := false
	start := 1
	for end := 1; end <= len(p); end++ {
		if end < len(p) && p[end] != '/' {
			continue
		}
		name, cur := p[start:end], p[:end]
		start = end + 1
		next, serr := w.step(fd, name, cur)
		if mk && errors.Is(serr, fs.ErrNotExist) {
			next, serr = w.makeDir(fd, name, cur)
			made = made || serr == nil
		}
		if serr = errors.Join(serr, w.r.closeFD(fd, cur)); serr != nil {
			if next >= 0 {
				serr = errors.Join(serr, w.r.closeFD(next, cur))
			}
			return -1, false, serr
		}
		fd = next
	}
	return fd, made, nil
}

// step opens the directory name in dirfd by the rules and checks it.
func (w *Writer) step(dirfd int, name, full string) (int, error) {
	fd, err := w.r.openBelow(dirfd, name, full, dirFlags, 0, writeResolve)
	if err != nil {
		return -1, err
	}
	info, err := w.r.statFD(fd, full)
	switch {
	case err != nil:
	case info.UID != 0:
		err = fmt.Errorf("%w: %s is not root's", ErrRefused, full)
	case info.Mode.Perm()&sharedWrite != 0:
		err = fmt.Errorf("%w: %s is writable by its group or others", ErrRefused, full)
	}
	if err != nil {
		return -1, errors.Join(err, w.r.closeFD(fd, full))
	}
	return fd, nil
}

// makeDir makes the directory name in parent, 0755 root, syncs parent, and is its descriptor.
func (w *Writer) makeDir(parent int, name, full string) (int, error) {
	if err := w.r.sys.mkdirat(parent, name, dirMode); err != nil {
		return -1, fmt.Errorf("guestroot: mkdir %s: %w", full, err)
	}
	fd, err := w.r.openBelow(parent, name, full, dirFlags, 0, writeResolve)
	if err != nil {
		return -1, err
	}
	if err := errors.Join(w.own(fd, full, dirMode), w.sync(parent, full)); err != nil {
		return -1, errors.Join(err, w.r.closeFD(fd, full))
	}
	return fd, nil
}

// own makes fd root's with mode.
func (w *Writer) own(fd int, full string, mode fs.FileMode) error {
	if err := w.r.sys.fchown(fd, 0, 0); err != nil {
		return fmt.Errorf("guestroot: chown %s: %w", full, err)
	}
	if err := w.r.sys.fchmod(fd, uint32(mode.Perm())); err != nil {
		return fmt.Errorf("guestroot: chmod %s: %w", full, err)
	}
	return nil
}

func (w *Writer) sync(fd int, full string) error {
	if err := w.r.sys.fsync(fd); err != nil {
		return fmt.Errorf("guestroot: sync %s: %w", full, err)
	}
	return nil
}

func (w *Writer) writeIn(dfd int, full, name string, data []byte, mode fs.FileMode) (bool, error) {
	same, err := w.same(dfd, full, name, data, mode)
	if err != nil || same {
		return false, err
	}
	temp := tempPrefix + name
	if err := w.unlinkStale(dfd, temp, full); err != nil {
		return false, err
	}
	if err := w.createIn(dfd, temp, full, data, mode); err != nil {
		return false, err
	}
	if err := w.r.sys.renameat2(dfd, temp, dfd, name, 0); err != nil {
		return false, errors.Join(fmt.Errorf("guestroot: rename into %s: %w", full, err), w.unlinkStale(dfd, temp, full))
	}
	return true, w.sync(dfd, full)
}

// same is whether name in dfd is already a root-owned regular file of mode holding data. A
// link or any other inode there is not, and is replaced unopened.
func (w *Writer) same(dfd int, full, name string, data []byte, mode fs.FileMode) (bool, error) {
	pfd, err := w.r.openBelow(dfd, name, full, unix.O_PATH, 0, writeResolve)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ELOOP) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err := w.r.statFD(pfd, full)
	if err != nil || !info.Mode.IsRegular() || info.Mode.Perm() != mode.Perm() || info.UID != 0 || info.Size != int64(len(data)) {
		return false, errors.Join(err, w.r.closeFD(pfd, full))
	}
	fd, err := w.r.reopenFD(pfd, info, full)
	if err = errors.Join(err, w.r.closeFD(pfd, full)); err != nil {
		if fd >= 0 {
			err = errors.Join(err, w.r.closeFD(fd, full))
		}
		return false, err
	}
	got, err := w.r.readAll(fd, full, int64(len(data)))
	if err = errors.Join(err, w.r.closeFD(fd, full)); err != nil {
		return false, err
	}
	return string(got) == string(data), nil
}

// unlinkStale removes a temporary a stopped write left behind.
func (w *Writer) unlinkStale(dfd int, temp, full string) error {
	if err := w.r.sys.unlinkat(dfd, temp, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("guestroot: remove the temporary of %s: %w", full, err)
	}
	return nil
}

// createIn writes the new file name in dfd: an exclusive create, root's with mode, synced. A
// failed write removes it.
func (w *Writer) createIn(dfd int, name, full string, data []byte, mode fs.FileMode) error {
	fd, err := w.r.openBelow(dfd, name, full, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600, writeResolve)
	if err != nil {
		return err
	}
	err = w.own(fd, full, mode)
	if err == nil {
		err = w.writeAll(fd, full, data)
	}
	if err == nil {
		err = w.sync(fd, full)
	}
	if err = errors.Join(err, w.r.closeFD(fd, full)); err != nil {
		if uerr := w.r.sys.unlinkat(dfd, name, 0); uerr != nil {
			err = errors.Join(err, fmt.Errorf("guestroot: remove %s: %w", full, uerr))
		}
		return err
	}
	return nil
}

func (w *Writer) writeAll(fd int, full string, data []byte) error {
	for len(data) > 0 {
		n, err := w.r.sys.write(fd, data)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("guestroot: write %s: %w", full, err)
		}
		data = data[n:]
	}
	return nil
}

func (w *Writer) placeIn(pfd int, parent, name string, fill func(d *Dir) error) (bool, error) {
	full := join(parent, name)
	fd, err := w.step(pfd, name, full)
	if err == nil {
		return false, w.r.closeFD(fd, full)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	temp := tempPrefix + name
	tempFull := join(parent, temp)
	if _, rerr := w.removeIn(pfd, tempFull, temp, 0); rerr != nil {
		return false, rerr
	}
	tfd, err := w.makeDir(pfd, temp, tempFull)
	if err != nil {
		return false, err
	}
	var subs []int
	err = fill(&Dir{w: w, fd: tfd, full: tempFull, subs: &subs})
	if err == nil {
		err = w.sync(tfd, tempFull)
	}
	for _, sub := range subs {
		err = errors.Join(err, w.r.closeFD(sub, tempFull))
	}
	err = errors.Join(err, w.r.closeFD(tfd, tempFull))
	if err == nil {
		if rerr := w.r.sys.renameat2(pfd, temp, pfd, name, unix.RENAME_NOREPLACE); rerr != nil {
			err = fmt.Errorf("guestroot: rename into %s: %w", full, rerr)
		}
	}
	if err != nil {
		_, rmErr := w.removeIn(pfd, tempFull, temp, 0)
		return false, errors.Join(err, rmErr)
	}
	return true, w.sync(pfd, parent)
}

func (w *Writer) linkIn(dfd int, full, name, target string) (bool, error) {
	buf := make([]byte, linkMax)
	n, err := w.r.sys.readlinkat(dfd, name, buf)
	switch {
	case err == nil && string(buf[:n]) == target:
		return false, nil
	case err != nil && !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.EINVAL):
		return false, fmt.Errorf("guestroot: readlink %s: %w", full, err)
	}
	temp := tempPrefix + name
	if err := w.unlinkStale(dfd, temp, full); err != nil {
		return false, err
	}
	if err := w.r.sys.symlinkat(target, dfd, temp); err != nil {
		return false, fmt.Errorf("guestroot: link %s: %w", full, err)
	}
	if err := w.r.sys.renameat2(dfd, temp, dfd, name, 0); err != nil {
		return false, errors.Join(fmt.Errorf("guestroot: rename into %s: %w", full, err), w.unlinkStale(dfd, temp, full))
	}
	return true, w.sync(dfd, full)
}

// removeIn removes the directory name in pfd and all it holds, depth levels below the one
// RemoveTree was given.
func (w *Writer) removeIn(pfd int, full, name string, depth int) (bool, error) {
	if depth > maxTreeDepth {
		return false, fmt.Errorf("%w: %s is more than %d directories deep", ErrTooLarge, full, maxTreeDepth)
	}
	fd, err := w.r.openBelow(pfd, name, full, dirFlags, 0, writeResolve)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = w.emptyDir(fd, full, depth)
	if err = errors.Join(err, w.r.closeFD(fd, full)); err != nil {
		return false, err
	}
	if err := w.r.sys.unlinkat(pfd, name, unix.AT_REMOVEDIR); err != nil {
		return false, fmt.Errorf("guestroot: remove %s: %w", full, err)
	}
	return true, nil
}

// emptyDir removes everything in the directory fd: each entry unlinked (a link is removed,
// never followed), a directory removed whole.
func (w *Writer) emptyDir(fd int, full string, depth int) error {
	names, err := w.r.entries(fd, full, maxTreeEntries)
	if err != nil {
		return err
	}
	for _, n := range names {
		uerr := w.r.sys.unlinkat(fd, n, 0)
		if errors.Is(uerr, unix.EISDIR) {
			_, uerr = w.removeIn(fd, full+"/"+n, n, depth+1)
		}
		if uerr != nil {
			return fmt.Errorf("guestroot: remove in %s: %w", full, uerr)
		}
	}
	return nil
}

// checkName refuses a name that is not one plain directory entry, or is a write's temporary.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, tempPrefix) {
		return fmt.Errorf("%w: %q is not a plain name", ErrRefused, name)
	}
	return nil
}

// checkMode refuses a mode with more than permission bits.
func checkMode(mode fs.FileMode) error {
	if mode&^fs.ModePerm != 0 {
		return fmt.Errorf("%w: mode %v is more than permissions", ErrRefused, mode)
	}
	return nil
}

func join(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}
