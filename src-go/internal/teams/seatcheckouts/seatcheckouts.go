// Package seatcheckouts is the list of the host checkouts that hold agent team seats. A seat
// lives in its checkout (`.claude/ccy/pingbus/seats/`), and a session record names the
// checkout only while its session runs, so once the session ends nothing else says where the
// seat is. The launch block (`describe-seats --block`, after every seat take) and every pass of
// the board add the checkout; the board and shutdown read every checkout listed, so a seat
// whose session has ended still reads as free (Plan 00201 Task 2.29).
//
// The file is `seat-checkouts` beside the session registry's directory, one absolute path a
// line, mode 0600. An add appends: two writers adding the same checkout leave it twice, which
// reads as once. An add that finds a listed checkout gone from the disk writes the list again
// without it (whole, renamed into place). A reader skips a checkout with no seat directory.
package seatcheckouts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// isGone reports whether dir is no longer on the disk; a checkout that cannot be asked about
// (a permission error) is kept.
func isGone(dir string) bool {
	_, err := os.Lstat(dir)
	return errors.Is(err, fs.ErrNotExist)
}

const fileName = "seat-checkouts"

// List is the file of seat checkouts.
type List struct{ path string }

// Of is the list beside the session records of store.
func Of(store *registry.Store) *List {
	return &List{path: filepath.Join(filepath.Dir(store.Dir()), fileName)}
}

// Path is where the list is kept.
func (l *List) Path() string { return l.path }

// Read is every checkout listed, once each, in the order first added. No file is none; a line
// that is not an absolute, clean path is an error naming the file and the line.
func (l *List) Read() ([]string, error) {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the list of seat checkouts %s could not be read: %w", l.path, err)
	}
	var dirs []string
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) || filepath.Clean(line) != line {
			return nil, fmt.Errorf("the list of seat checkouts %s has %s on line %d, which is not an absolute path",
				l.path, strconv.Quote(line), i+1)
		}
		if !slices.Contains(dirs, line) {
			dirs = append(dirs, line)
		}
	}
	return dirs, nil
}

// Add lists dir, unless it is listed already.
func (l *List) Add(dir string) error {
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\n\r") {
		return fmt.Errorf("%s is not an absolute path on one line, so it cannot be listed in %s", strconv.Quote(dir), l.path)
	}
	dir = filepath.Clean(dir)
	listed, err := l.Read()
	if err != nil {
		return err
	}
	if slices.Contains(listed, dir) {
		return nil
	}
	kept := slices.DeleteFunc(slices.Clone(listed), isGone)
	if len(kept) < len(listed) {
		return fsx.WriteAtomic(l.path, "list of seat checkouts", []byte(strings.Join(append(kept, dir), "\n")+"\n"), 0o600)
	}
	if err = os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("the list of seat checkouts could not be written: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("the list of seat checkouts could not be written: %w", err)
	}
	_, werr := f.WriteString(dir + "\n")
	if err = errors.Join(werr, f.Close()); err != nil {
		return fmt.Errorf("the list of seat checkouts %s could not be written: %w", l.path, err)
	}
	return nil
}
