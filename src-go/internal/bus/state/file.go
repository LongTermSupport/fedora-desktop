// Package state reads and writes the files a pingbus member keeps, at v1's paths, in v1's
// formats and under v1's locks, so a v1 (Python) process and a v2 (Go) process can take turns
// on one seat, one state directory and one PINGBUS_HOME (Plan 00209 SPEC-state.md, S1-S45 and
// L1-L12). A reader accepts what v1 writes and refuses the rest with an *Error of one Class,
// naming the file and never quoting a value read from it; a writer writes the bytes v1's writer
// writes, through one atomic writer with v1's temporary name.
//
// The protocol's grammars (team, seat, handle, user ID, room and event IDs, verbs, reasons) are
// bus/proto's; until it lands this package keeps minimal copies in grammar.go, each marked
// TODO(bus/proto).
package state

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// Class is why a file was refused (SPEC section 2); the corpus names these.
type Class string

const (
	// ClassVersion is a `v` (or `protocol`) that is an integer this reader does not accept.
	ClassVersion Class = "version"
	// ClassUnknownKey is an object with a key its format does not name.
	ClassUnknownKey Class = "unknown-key"
	// ClassMissingKey is an object without a key its format requires.
	ClassMissingKey Class = "missing-key"
	// ClassDuplicateKey is an object that names one key twice.
	ClassDuplicateKey Class = "duplicate-key"
	// ClassType is a value of the wrong JSON type (a bool or float where an integer is due).
	ClassType Class = "type"
	// ClassGrammar is a string or integer of the right type that fails its grammar or bound.
	ClassGrammar Class = "grammar"
	// ClassSize is a file, list or map over its cap.
	ClassSize Class = "size"
	// ClassSyntax is text that is not UTF-8, not JSON, or not the line format of its file.
	ClassSyntax Class = "syntax"
	// ClassFile is not a regular file, a symlink, or the wrong owner or mode.
	ClassFile Class = "file"
)

// Error is a file refused: its path, the class and what is wrong, never a value from it.
type Error struct {
	Path  string
	Class Class
	Msg   string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Path, e.Msg, e.Class) }

func refuse(path string, class Class, msg string) error {
	return &Error{Path: path, Class: class, Msg: msg}
}

const (
	fileMode = 0o600
	dirMode  = 0o700
	// tmpHex is the number of random bytes in a temporary's name: 16 hex digits.
	tmpHex = 8
)

// readRegular is a regular file's bytes, read without following a symlink or blocking on a
// FIFO, of at most limit bytes (S4). found is false when the file is absent; an I/O failure
// that says nothing of the file's format is returned unclassed.
func readRegular(path string, limit int) ([]byte, bool, error) {
	file, err := openRegular(path, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer closeQuietly(file)
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > limit {
		return nil, false, refuse(path, ClassSize, fmt.Sprintf("larger than %d bytes", limit))
	}
	return raw, true, nil
}

// openRegular opens path with flags plus O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC and refuses what is
// not a regular file. os.ErrNotExist is returned as it is.
func openRegular(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, fileMode)
	switch {
	case errors.Is(err, unix.ELOOP):
		return nil, refuse(path, ClassFile, "a symlink")
	case errors.Is(err, unix.ENOENT):
		return nil, fmt.Errorf("open %s: %w", path, os.ErrNotExist)
	case err != nil:
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		closeQuietly(file)
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		closeQuietly(file)
		return nil, refuse(path, ClassFile, "not a regular file")
	}
	return file, nil
}

// closeQuietly closes a file only read from, or one whose error is already being returned: a
// close cannot lose data that was not written.
func closeQuietly(file *os.File) {
	if err := file.Close(); err != nil {
		return
	}
}

// tmpName is v1's temporary beside name: `.<name>.<16 lowercase hex>.tmp` (S15), the one shape
// v1's inbox listing skips.
func tmpName(name string) (string, error) {
	b := make([]byte, tmpHex)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("a temporary name: %w", err)
	}
	return "." + name + "." + hex.EncodeToString(b) + ".tmp", nil
}

// writeAtomic replaces path with data (S15): a temporary opened O_EXCL|O_NOFOLLOW with mode
// 0600 and fchmod to it (SB8), written, fsynced, renamed over path, then the directory fsynced.
// The temporary is removed on any failure.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	name, err := tmpName(filepath.Base(path))
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, name)
	fd, err := unix.Open(tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	file := os.NewFile(uintptr(fd), tmp)
	if err := fill(file, data); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return fsyncDir(dir)
}

// fill sets the mode, writes, fsyncs and closes the temporary.
func fill(file *os.File, data []byte) error {
	err := file.Chmod(fileMode)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

// fsyncDir makes a directory's entries durable.
func fsyncDir(dir string) error {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	return errors.Join(unix.Fsync(fd), unix.Close(fd))
}

// decodeJSON is json.loads(raw.decode("utf-8")) with every object's key order kept and a
// duplicate key refused (S5, SB1).
func decodeJSON(path string, raw []byte) (any, error) {
	v, err := pyjson.DecodeUTF8(raw, pyjson.Options{NoDuplicateKeys: true, Ordered: true})
	switch {
	case errors.Is(err, pyjson.ErrDuplicateKey):
		return nil, refuse(path, ClassDuplicateKey, "an object names a key twice")
	case errors.Is(err, pyjson.ErrNotUTF8):
		return nil, refuse(path, ClassSyntax, "not UTF-8")
	case err != nil:
		return nil, refuse(path, ClassSyntax, "not JSON")
	}
	return v, nil
}

// object is v as an object, or a type refusal.
func object(path string, v any) (pyjson.Ordered, error) {
	o, ok := v.(pyjson.Ordered)
	if !ok {
		return nil, refuse(path, ClassType, "not a JSON object")
	}
	return o, nil
}
