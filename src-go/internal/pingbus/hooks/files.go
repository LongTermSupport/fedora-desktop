package hooks

// Future home: bus/state. How the Python pingbus opens, reads and replaces its files: the
// readers never follow a symlink unless the Python does and never block on a FIFO; the
// writers go through a temporary file beside the target, renamed over it.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

var (
	// errNotRegular is a path that opened but is not a regular file.
	errNotRegular = errors.New("not a regular file")
	// errTooLarge is a file longer than the limit its reader allows.
	errTooLarge = errors.New("too large")
)

// opened is a file read whole: its bytes and the stat of the descriptor they came from.
type opened struct {
	raw  []byte
	info syscall.Stat_t
}

// readRegular is inbox._read_regular: O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC (a symlink is
// ELOOP), a regular file (errNotRegular) of at most limit bytes (errTooLarge). An open or read
// failure is its errno.
func readRegular(path string, limit int64) (opened, error) {
	return readOpen(path, syscall.O_NOFOLLOW, limit)
}

// readFollowing is readRegular through a symlink, as config._read_member_file opens.
func readFollowing(path string, limit int64) (opened, error) {
	return readOpen(path, 0, limit)
}

func readOpen(path string, extra int, limit int64) (opened, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|extra, 0)
	if err != nil {
		return opened{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	got, readErr := readOpened(file, limit)
	closeErr := file.Close()
	if readErr != nil {
		return opened{}, readErr
	}
	return got, closeErr
}

func readOpened(file *os.File, limit int64) (opened, error) {
	var got opened
	if err := syscall.Fstat(int(file.Fd()), &got.info); err != nil {
		return opened{}, err
	}
	if got.info.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return opened{}, errNotRegular
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return opened{}, err
	}
	if int64(len(raw)) > limit {
		return opened{}, errTooLarge
	}
	got.raw = raw
	return got, nil
}

func isErrno(err error, want syscall.Errno) bool {
	errno, ok := errors.AsType[syscall.Errno](err)
	return ok && errno == want
}

// pyJoin is pathlib's join of base and rel: rel when it is absolute; then empty and "."
// components dropped and ".." kept, as pathlib leaves them for the kernel.
func pyJoin(base, rel string) string {
	joined := rel
	if !strings.HasPrefix(rel, "/") {
		joined = base + "/" + rel
	}
	var parts []string
	for part := range strings.SplitSeq(joined, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	root := ""
	switch {
	case strings.HasPrefix(joined, "//") && !strings.HasPrefix(joined, "///"):
		root = "//"
	case strings.HasPrefix(joined, "/"):
		root = "/"
	}
	if len(parts) == 0 {
		if root == "" {
			return "."
		}
		return root
	}
	return root + strings.Join(parts, "/")
}

// parentOf is pathlib's parent of a joined path.
func parentOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return path[:i]
}

// baseOf is pathlib's name of a joined path.
func baseOf(path string) string {
	return path[strings.LastIndexByte(path, '/')+1:]
}

// tempName is `.<name>.<16 hex>.tmp` beside path, the writers' temporary file.
func tempName(path string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return pyJoin(parentOf(path), "."+baseOf(path)+"."+hex.EncodeToString(nonce[:])+".tmp"), nil
}

// replaceFile writes data to a fresh temporary file (O_EXCL|O_NOFOLLOW, 0600), fchmods it 0600
// when chmod, fsyncs it, and renames it over path; the temporary file is removed on failure.
func replaceFile(path string, data []byte, chmod bool) error {
	temp, err := tempName(path)
	if err != nil {
		return err
	}
	fd, err := syscall.Open(temp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, fileMode)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temp)
	err = fillFile(file, data, chmod)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp, path)
	}
	if err != nil {
		if removeErr := os.Remove(temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return errors.Join(err, removeErr)
		}
		return unwrapPath(err)
	}
	return nil
}

func fillFile(file *os.File, data []byte, chmod bool) error {
	if chmod {
		if err := file.Chmod(fileMode); err != nil {
			return err
		}
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// unwrapPath is the errno inside an *os.PathError or *os.LinkError, so callers can test it.
func unwrapPath(err error) error {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		return errno
	}
	return err
}

// fsyncDir is inbox._fsync_dir.
func fsyncDir(path string) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	err = file.Sync()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// writeAtomic is inbox._write_atomic: the bytes through a fchmod-ed, fsynced temporary file.
func writeAtomic(path string, data []byte) error {
	return replaceFile(path, data, true)
}

// writeJSONFile is inbox.write_json_file (H88): compact JSON, then the directory fsynced.
func writeJSONFile(path string, value any) error {
	text, err := pyDump(value, styleCompact)
	if err != nil {
		return err
	}
	if err := writeAtomic(path, []byte(text)); err != nil {
		return err
	}
	return fsyncDir(parentOf(path))
}

// writePrivateJSON is membership.write_private_json (H83): sorted keys, default separators
// and a newline, through a fsynced temporary file (no fchmod, no directory fsync).
func writePrivateJSON(path string, value any) error {
	text, err := pyDump(value, styleSorted)
	if err != nil {
		return err
	}
	return replaceFile(path, []byte(text+"\n"), false)
}
