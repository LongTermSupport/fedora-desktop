package tarstate

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Write puts the state of the team directory dir into a tar on w: secrets/ (every file and
// directory below it), team.json, and registry.json and room_id when present. The
// directory belongs to the agent-bus user, so it is read as hostile: through an os.Root,
// never following a symlink, and a member that is anything but a plain file or directory
// is an error ("backup: <path> is a <kind>, not a regular file"), not a stored link.
//
// Members are written 0600 / 0700 with no owner, and the finished tar is passed through
// Read with the same limits before any byte reaches w, so Write cannot produce a tar that
// Read would refuse.
func Write(w io.Writer, dir string, limits Limits) error {
	lim := limits.withDefaults()
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	var buf bytes.Buffer
	buildErr := assemble(&buf, root, lim)
	if closeErr := root.Close(); closeErr != nil {
		return errors.Join(buildErr, fmt.Errorf("backup: closing %s: %w", dir, closeErr))
	}
	if buildErr != nil {
		return buildErr
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// assemble writes the tar of root into buf and checks it with Read.
func assemble(buf *bytes.Buffer, root *os.Root, lim Limits) error {
	b := &builder{root: root, tw: tar.NewWriter(buf), lim: lim}
	if err := b.all(); err != nil {
		return err
	}
	if err := b.tw.Close(); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if _, err := Read(bytes.NewReader(buf.Bytes()), lim); err != nil {
		return fmt.Errorf("backup: the state would not pass the restore check: %w", err)
	}
	return nil
}

type builder struct {
	root    *os.Root
	tw      *tar.Writer
	lim     Limits
	entries int
	total   int64
}

func (b *builder) all() error {
	if err := b.file("team.json", true); err != nil {
		return err
	}
	for _, name := range []string{"registry.json", "room_id"} {
		if err := b.file(name, false); err != nil {
			return err
		}
	}
	return b.tree("secrets")
}

// kind names a file mode for the refusal message.
func kind(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "directory"
	case m&fs.ModeNamedPipe != 0:
		return "named pipe"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeDevice != 0:
		return "device"
	case m.IsRegular():
		return "regular file"
	default:
		return "irregular file"
	}
}

func (b *builder) count(rel string) error {
	b.entries++
	if b.entries > b.lim.MaxEntries {
		return fmt.Errorf("backup: more than %d members", b.lim.MaxEntries)
	}
	if strings.Count(rel, "/")+1 > b.lim.MaxDepth {
		return fmt.Errorf("backup: %s is deeper than %d components", rel, b.lim.MaxDepth)
	}
	return nil
}

// file adds a regular file; an absent optional one is skipped, an absent required one is
// an error, and a symlink (even a dangling one) is always an error.
func (b *builder) file(rel string, required bool) error {
	info, err := b.root.Lstat(rel)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup: %s is a %s, not a regular file", rel, kind(info.Mode()))
	}
	return b.addFile(rel)
}

func (b *builder) addFile(rel string) error {
	if err := b.count(rel); err != nil {
		return err
	}
	got, err := readRegular(b.root, rel, b.lim.MaxFileSize)
	if err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	b.total += int64(len(got.data))
	if b.total > b.lim.MaxTotal {
		return fmt.Errorf("backup: the files are over the %d byte total limit", b.lim.MaxTotal)
	}
	hdr := &tar.Header{Name: rel, Typeflag: tar.TypeReg, Mode: tarFileMode, Size: int64(len(got.data)), ModTime: got.modTime}
	if err = b.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	if _, err = b.tw.Write(got.data); err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	return nil
}

// The modes every member is written with, whatever the file system says: a restore applies its
// own and ignores the tar's.
const (
	tarFileMode = 0o600
	tarDirMode  = 0o700
)

// tree adds the directory rel and everything below it.
func (b *builder) tree(rel string) error {
	info, err := b.root.Lstat(rel)
	if err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup: %s is a %s, not a directory", rel, kind(info.Mode()))
	}
	if err = b.count(rel); err != nil {
		return err
	}
	hdr := &tar.Header{Name: rel, Typeflag: tar.TypeDir, Mode: tarDirMode, ModTime: info.ModTime().UTC().Truncate(time.Second)}
	if err = b.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	names, err := listDir(b.root, rel)
	if err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	for _, name := range names {
		if err := b.child(path.Join(rel, name)); err != nil {
			return err
		}
	}
	return nil
}

// listDir is the sorted names in the directory rel, which is opened without following a
// symlink at its last component.
func listDir(root *os.Root, rel string) ([]string, error) {
	d, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	entries, readErr := d.ReadDir(-1)
	if closeErr := d.Close(); closeErr != nil || readErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	slices.Sort(names)
	return names, nil
}

// child adds a member below secrets/: a directory recurses, a plain file is stored,
// anything else is refused.
func (b *builder) child(rel string) error {
	info, err := b.root.Lstat(rel)
	if err != nil {
		return fmt.Errorf("backup: %s: %w", rel, err)
	}
	switch {
	case info.IsDir():
		return b.tree(rel)
	case info.Mode().IsRegular():
		return b.addFile(rel)
	default:
		return fmt.Errorf("backup: %s is a %s, not a regular file", rel, kind(info.Mode()))
	}
}

type regular struct {
	data    []byte
	modTime time.Time
}

// readRegular reads a regular file of at most limit bytes. An os.Root follows a symlink in
// the last component as long as it stays inside the root, so the file is lstat-ed first and
// the opened file must be that very file (same device and inode): a name swapped for a
// symlink, a device or another file between the two is refused, not read.
func readRegular(root *os.Root, rel string, limit int64) (regular, error) {
	before, err := root.Lstat(rel)
	if err != nil {
		return regular{}, fmt.Errorf("lstat: %w", err)
	}
	if !before.Mode().IsRegular() {
		return regular{}, fmt.Errorf("is a %s, not a regular file", kind(before.Mode()))
	}
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return regular{}, fmt.Errorf("open: %w", err)
	}
	got, readErr := readOpened(f, before, limit)
	if closeErr := f.Close(); closeErr != nil || readErr != nil {
		return regular{}, errors.Join(readErr, closeErr)
	}
	return got, nil
}

func readOpened(f *os.File, before fs.FileInfo, limit int64) (regular, error) {
	info, err := f.Stat()
	if err != nil {
		return regular{}, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return regular{}, fmt.Errorf("changed while being read (now a %s)", kind(info.Mode()))
	}
	if info.Size() > limit {
		return regular{}, fmt.Errorf("%d bytes is over the %d byte file limit", info.Size(), limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return regular{}, fmt.Errorf("read: %w", err)
	}
	if int64(len(data)) > limit {
		return regular{}, fmt.Errorf("grew past the %d byte file limit while being read", limit)
	}
	return regular{data: data, modTime: info.ModTime().UTC().Truncate(time.Second)}, nil
}
