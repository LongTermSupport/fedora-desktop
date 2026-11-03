// Package testfiles is how a test reads and opens the files the code under test made beneath a
// directory the test owns (its t.TempDir, or a testdata directory): by name, through an os.Root
// on that directory, so a name that leaves the directory (a .., a full path elsewhere, a link
// that leads out) is refused instead of opened. A test holds the full path of what it made more
// often than a relative name, so a name may also be a full path under the directory.
//
// The helpers end the test on a failure, except Try. They hand back an open file for the caller to close:
// the caller already says when, and some tests hand the file to code that closes it.
package testfiles

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Read is the content of name beneath dir.
func Read(tb testing.TB, dir, name string) []byte {
	tb.Helper()
	data, err := readIn(dir, name)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// Find is Read, except that found is false, and the test goes on, when there is no such file.
func Find(tb testing.TB, dir, name string) ([]byte, bool) {
	tb.Helper()
	data, err := readIn(dir, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		tb.Fatal(err)
	}
	return data, true
}

// Try is Read, except that it never ends the test: for a diagnostic that reports a file, or why
// it cannot be read.
func Try(dir, name string) ([]byte, error) { return readIn(dir, name) }

// Open is name beneath dir, open for reading.
func Open(tb testing.TB, dir, name string) *os.File {
	tb.Helper()
	return OpenFile(tb, dir, name, os.O_RDONLY, 0)
}

// OpenFile is name beneath dir, opened as os.OpenFile opens it, except that a link is followed
// only to a file that is also beneath dir.
func OpenFile(tb testing.TB, dir, name string, flag int, perm fs.FileMode) *os.File {
	tb.Helper()
	f, err := openIn(dir, name, flag, perm)
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

// Executable is the running test binary, open for reading. Nothing owns the directory it was
// built in, so it is opened in its own directory, by its own name.
func Executable(tb testing.TB) *os.File {
	tb.Helper()
	self, err := os.Executable()
	if err != nil {
		tb.Fatal(err)
	}
	return Open(tb, filepath.Dir(self), filepath.Base(self))
}

func readIn(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	data, err := root.ReadFile(beneath(dir, name))
	return data, errors.Join(err, root.Close())
}

func openIn(dir, name string, flag int, perm fs.FileMode) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	f, err := root.OpenFile(beneath(dir, name), flag, perm)
	if closeErr := root.Close(); closeErr != nil {
		if f != nil {
			closeErr = errors.Join(closeErr, f.Close())
		}
		return nil, errors.Join(err, closeErr)
	}
	return f, err
}

// beneath is name as a name relative to dir when it is a full path under it. A full path
// elsewhere stays a full path, and the root refuses it.
func beneath(dir, name string) string {
	if !filepath.IsAbs(name) {
		return name
	}
	rel, err := filepath.Rel(dir, name)
	if err != nil {
		return name
	}
	return rel
}
