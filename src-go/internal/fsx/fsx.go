// Package fsx is the file writes the tools share. FS holds the primitives the installer
// writes as root with: every write goes through an os.Root (so a symlink swapped into a
// directory another user can write cannot redirect it), is atomic (temp file and rename),
// refuses to follow a symlink at the destination, and reports each change as a CHANGED line.
// WriteAtomic and RemoveIfPresent are the plain atomic replace and tolerant removal the ccy
// tools use on their own files.
package fsx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
)

// tempMarker is the infix of the sibling temp files; a failed run removes its own and the
// next run replaces a stale one.
const tempMarker = ".agent-bus-install."

// FS writes below one directory. Names are slash-separated and relative to that directory;
// every operation goes through an os.Root, so a name or a symlink on the way cannot lead
// outside it (a symlink that stays inside, such as /usr/local/sbin -> bin, is followed).
// Mode arguments are Go file modes: pass the setuid, setgid and sticky bits as
// fs.ModeSetuid, fs.ModeSetgid and fs.ModeSticky.
type FS struct {
	root    *os.Root
	display string
	out     io.Writer
	pid     int
}

// Open roots an FS at dir. display is the absolute path of dir as it should appear in
// messages ("" when dir is the file system root); out receives the CHANGED lines; pid
// names the temp files.
func Open(dir, display string, out io.Writer, pid int) (*FS, error) {
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	return &FS{root: root, display: display, out: out, pid: pid}, nil
}

// Close releases the directory.
func (f *FS) Close() error {
	if err := f.root.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.shown("."), err)
	}
	return nil
}

// EnsureDir creates the directory name (its parent must exist) with the mode and owner,
// or corrects the mode and owner of an existing one. A symlink or file in the way is an
// error. It reports whether it changed anything.
func (f *FS) EnsureDir(name string, mode fs.FileMode, own Owner) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	info, err := f.root.Lstat(name)
	switch {
	case err == nil && !info.IsDir():
		return false, fmt.Errorf("%s exists and is not a directory", f.shown(name))
	case err == nil:
		return f.fixDir(name, info, mode, own)
	case errors.Is(err, fs.ErrNotExist):
		return f.makeDir(name, mode, own)
	default:
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
}

// RequireSystemDir checks that name is a directory (a symlink to one counts) without
// touching its mode or owner.
func (f *FS) RequireSystemDir(name string) error {
	info, err := f.root.Stat(name)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory on this host", f.shown(name))
	}
	return nil
}

// PutBytes installs data as the file name; see PutFile.
func (f *FS) PutBytes(label string, data []byte, name string, mode fs.FileMode, own Owner) (bool, error) {
	return f.put(label, func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }, name, mode, own)
}

// PutFile installs the file at src as name, atomically (a sibling temp file renamed over
// it), when its bytes, mode or owner differ from what is there. It refuses to write
// through a symlink at name. On error nothing is left behind and the old file is intact.
// It reports whether it changed anything and prints CHANGED<TAB>label when it did.
func (f *FS) PutFile(label, src, name string, mode fs.FileMode, own Owner) (bool, error) {
	return f.put(label, func() (io.ReadCloser, error) { return os.Open(filepath.Clean(src)) }, name, mode, own)
}

// PutSymlink makes name a symlink to target (stored as given), atomically, when it is not
// one already. A regular file or directory in the way is an error; a symlink with another
// target is replaced. It reports whether it changed anything and prints CHANGED<TAB>label.
func (f *FS) PutSymlink(label, target, name string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	same, err := f.linkIsCurrent(target, name)
	if err != nil || same {
		return false, err
	}
	tmp := f.tempName(name)
	if err := f.root.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	if err := f.root.Symlink(target, tmp); err != nil {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	if err := f.root.Rename(tmp, name); err != nil {
		return false, f.dropTemp(tmp, fmt.Errorf("%s: %w", f.shown(name), err))
	}
	return true, f.changed(label)
}

// shown is the absolute path of name as it appears in messages.
func (f *FS) shown(name string) string {
	if name == "." {
		if f.display == "" {
			return "/"
		}
		return f.display
	}
	return f.display + "/" + name
}

func (f *FS) changed(text string) error {
	if _, err := fmt.Fprintf(f.out, "CHANGED\t%s\n", text); err != nil {
		return fmt.Errorf("writing the change report: %w", err)
	}
	return nil
}

func (f *FS) makeDir(name string, mode fs.FileMode, own Owner) (bool, error) {
	if err := f.root.Mkdir(name, 0o700); err != nil {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	if err := f.settleDir(name, nil, mode, own); err != nil {
		return false, f.dropDir(name, err)
	}
	return true, f.changed("directory " + f.shown(name))
}

// dropDir removes a directory made by a run that then failed, and returns the failure.
func (f *FS) dropDir(name string, cause error) error {
	if err := f.root.Remove(name); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (f *FS) fixDir(name string, info fs.FileInfo, mode fs.FileMode, own Owner) (bool, error) {
	if matches(info, mode, own) {
		return false, nil
	}
	if err := f.settleDir(name, info, mode, own); err != nil {
		return false, err
	}
	return true, f.changed("directory " + f.shown(name) + " mode and owner")
}

// settleDir sets owner then mode through an open descriptor, never through the name, so
// the directory cannot be swapped for a symlink between the check and the change. before,
// when not nil, is the lstat the opened directory must still match.
func (f *FS) settleDir(name string, before fs.FileInfo, mode fs.FileMode, own Owner) (err error) {
	d, err := f.root.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("%s: %w", f.shown(name), err)
	}
	defer func() {
		if closeErr := d.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("%s: %w", f.shown(name), closeErr)
		}
	}()
	if before != nil {
		opened, statErr := d.Stat()
		if statErr != nil {
			return fmt.Errorf("%s: %w", f.shown(name), statErr)
		}
		if !os.SameFile(before, opened) {
			return fmt.Errorf("%s changed while it was being fixed", f.shown(name))
		}
	}
	if err := d.Chown(own.UID, own.GID); err != nil {
		return fmt.Errorf("%s: %w", f.shown(name), err)
	}
	if err := d.Chmod(mode); err != nil {
		return fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return nil
}

func (f *FS) put(label string, open func() (io.ReadCloser, error), name string, mode fs.FileMode, own Owner) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	same, err := f.unchanged(open, name, mode, own)
	if err != nil || same {
		return false, err
	}
	src, err := open()
	if err != nil {
		return false, fmt.Errorf("%s: %w", label, err)
	}
	tmp := f.tempName(name)
	if err := f.writeTemp(tmp, src, mode, own); err != nil {
		return false, f.dropTemp(tmp, fmt.Errorf("%s: %w", f.shown(name), err))
	}
	if err := f.root.Rename(tmp, name); err != nil {
		return false, f.dropTemp(tmp, fmt.Errorf("%s: %w", f.shown(name), err))
	}
	return true, f.changed(label)
}

func (f *FS) tempName(name string) string {
	return path.Join(path.Dir(name), "."+path.Base(name)+tempMarker+strconv.Itoa(f.pid))
}

// dropTemp removes a temp file after a failure and returns the failure, joined with any
// trouble removing it.
func (f *FS) dropTemp(tmp string, cause error) error {
	if err := f.root.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(cause, err)
	}
	return cause
}

// linkIsCurrent reports whether name is already a symlink to target; a file or directory
// there is an error.
func (f *FS) linkIsCurrent(target, name string) (bool, error) {
	info, err := f.root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	case info.Mode()&fs.ModeSymlink == 0:
		return false, fmt.Errorf("%s exists and is not a symlink", f.shown(name))
	}
	current, err := f.root.Readlink(name)
	if err != nil {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return current == target, nil
}

// unchanged reports whether name is a regular file with the wanted bytes, mode and owner.
// A symlink or a non-regular file there is an error.
func (f *FS) unchanged(open func() (io.ReadCloser, error), name string, mode fs.FileMode, own Owner) (bool, error) {
	info, err := f.root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	case info.Mode()&fs.ModeSymlink != 0:
		return false, fmt.Errorf("%s is a symlink; refusing to write through it", f.shown(name))
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%s exists and is not a regular file", f.shown(name))
	case !matches(info, mode, own):
		return false, nil
	}
	cur, err := f.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	src, err := open()
	if err != nil {
		return false, errors.Join(fmt.Errorf("%s: %w", f.shown(name), err), cur.Close())
	}
	same, cmpErr := sameContent(cur, src)
	if closeErr := errors.Join(cur.Close(), src.Close()); closeErr != nil || cmpErr != nil {
		return false, errors.Join(cmpErr, closeErr)
	}
	return same, nil
}

// writeTemp creates tmp exclusively (replacing a stale one without following it), fills it
// from src and sets owner and mode on the open descriptor. src is closed.
func (f *FS) writeTemp(tmp string, src io.ReadCloser, mode fs.FileMode, own Owner) error {
	if err := f.root.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(err, src.Close())
	}
	out, err := f.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.Join(err, src.Close())
	}
	fillErr := fill(out, src, mode, own)
	return errors.Join(fillErr, src.Close(), out.Close())
}

func fill(out *os.File, src io.Reader, mode fs.FileMode, own Owner) error {
	if _, err := io.Copy(out, src); err != nil {
		return err
	}
	if err := out.Chown(own.UID, own.GID); err != nil {
		return err
	}
	if err := out.Chmod(mode); err != nil {
		return err
	}
	return out.Sync()
}

// sameContent compares two streams without holding either in memory.
func sameContent(a, b io.Reader) (bool, error) {
	bufA, bufB := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		na, errA := io.ReadFull(a, bufA)
		nb, errB := io.ReadFull(b, bufB)
		endA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		endB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		switch {
		case errA != nil && !endA:
			return false, fmt.Errorf("read: %w", errA)
		case errB != nil && !endB:
			return false, fmt.Errorf("read: %w", errB)
		case na != nb || !bytes.Equal(bufA[:na], bufB[:nb]):
			return false, nil
		case endA && endB:
			return true, nil
		case endA != endB:
			return false, nil
		}
	}
}

func checkName(name string) error {
	if name == "." || !fs.ValidPath(name) {
		return fmt.Errorf("%q is not a path inside the directory", name)
	}
	return nil
}

// unixBits is the permission, setuid, setgid and sticky bits of a mode as chmod writes them.
func unixBits(m fs.FileMode) uint32 {
	bits := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits
}

func ownerOf(info fs.FileInfo) (Owner, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Owner{}, false
	}
	return Owner{UID: int(st.Uid), GID: int(st.Gid)}, true
}

// matches: the file already has this mode and owner.
func matches(info fs.FileInfo, mode fs.FileMode, own Owner) bool {
	have, ok := ownerOf(info)
	return ok && have == own && unixBits(info.Mode()) == unixBits(mode)
}
