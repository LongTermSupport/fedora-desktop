package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// Sub roots a new FS at the directory name below f. The child is opened through f's root, so
// a symlink that leaves f is refused; its display path continues f's. The caller closes it.
func (f *FS) Sub(name string) (*FS, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	root, err := f.root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return &FS{root: root, display: f.shown(name), out: f.out, pid: f.pid}, nil
}

// Root is the directory this FS writes below, for a reader that takes an os.Root so it looks
// at the directory through the same descriptor. The FS still owns it: do not close it.
func (f *FS) Root() *os.Root { return f.root }

// Display is the absolute path of name as messages show it.
func (f *FS) Display(name string) string { return f.shown(name) }

// Quiet is f writing without CHANGED lines, for the records that are not a change to report.
// It shares f's directory, so only f is closed.
func (f *FS) Quiet() *FS {
	quiet := *f
	quiet.out = io.Discard
	return &quiet
}

// ReadFile is the content of the file name, read through the root.
func (f *FS) ReadFile(name string) ([]byte, error) {
	data, err := fs.ReadFile(f.root.FS(), name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return data, nil
}

// Open opens the file name for reading; the caller closes it.
func (f *FS) Open(name string) (*os.File, error) {
	file, err := f.root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return file, nil
}

// ReadDir lists the directory name ("." is the root itself).
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.root.FS(), name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return entries, nil
}

// Lstat describes name without following a final symlink.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	info, err := f.root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return info, nil
}

// Stat describes name, following symlinks that stay inside the root.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	info, err := f.root.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return info, nil
}

// RemoveIfPresent removes the file or empty directory name, and reports whether it was
// there. A path outside the root is an error, not an absence.
func (f *FS) RemoveIfPresent(name string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	err := f.root.Remove(name)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
}

// RemoveAll removes name and everything below it, never following a symlink; what is not
// there is not an error.
func (f *FS) RemoveAll(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := f.root.RemoveAll(name); err != nil {
		return fmt.Errorf("%s: %w", f.shown(name), err)
	}
	return nil
}

// EnsureFileMode corrects the mode and owner of the existing regular file name and leaves
// its content alone, reporting CHANGED<TAB>label when it had to. A symlink or another kind
// of file is an error.
func (f *FS) EnsureFileMode(label, name string, mode fs.FileMode, own Owner) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	info, err := f.root.Lstat(name)
	switch {
	case err != nil:
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%s exists and is not a regular file", f.shown(name))
	case matches(info, mode, own):
		return false, nil
	}
	file, err := f.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("%s: %w", f.shown(name), err)
	}
	fixErr := fixOpened(file, info, mode, own)
	if closeErr := file.Close(); closeErr != nil || fixErr != nil {
		return false, errors.Join(wrapPath(f.shown(name), fixErr), wrapPath(f.shown(name), closeErr))
	}
	return true, f.changed(label)
}

// fixOpened sets owner then mode through the descriptor, once it is the file that was seen.
func fixOpened(file *os.File, before fs.FileInfo, mode fs.FileMode, own Owner) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, opened) {
		return errors.New("changed while it was being fixed")
	}
	if err := file.Chown(own.UID, own.GID); err != nil {
		return err
	}
	return file.Chmod(mode)
}

func wrapPath(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", path, err)
}
