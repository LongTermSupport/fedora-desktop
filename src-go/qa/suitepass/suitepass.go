// Package suitepass remembers that a test suite passed over a given set of inputs, so a local
// qa-changed run does not run it again over the same inputs. A suite that builds a Go command
// and drives it end to end (scripts/go-binary-suites.tsv) is picked because a file in the
// command's build closure changed; if nothing in that closure and nothing of the suite itself
// has changed since a recorded pass, running it again proves nothing new.
//
// The key is a SHA-256 over every input file (its repository-relative path and its content,
// each framed by its length, so a path and content cannot be traded) and the extra words the
// caller adds (the suite's command line, the Go toolchain). A file that does not exist is an
// input too, recorded as missing. The store is one empty file per key in a directory outside
// the repository: QA_SUITE_PASSES, else $XDG_CACHE_HOME, else $HOME/.cache. It is local state
// only; CI never reads it.
package suitepass

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Key is the digest of the files (repository-relative, under root) and the extra words. The
// order of the files does not matter and a file named twice counts once. A file that cannot be
// read (a directory, no permission) is an error, never a key.
func Key(root string, files []string, extra ...string) (string, error) {
	sorted := slices.Clone(files)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	var buf []byte
	frame := func(kind, text string) {
		buf = fmt.Appendf(buf, "%s %d\n%s\n", kind, len(text), text)
	}
	for _, word := range extra {
		frame("extra", word)
	}
	for _, rel := range sorted {
		frame("path", rel)
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			frame("missing", "")
		case err != nil:
			return "", fmt.Errorf("cannot read the suite input %s: %w", rel, err)
		default:
			sum := sha256.Sum256(body)
			frame("content", hex.EncodeToString(sum[:]))
		}
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// Store is the directory of recorded passes.
type Store struct {
	Dir string
}

// storeDirMode keeps the record private to the user.
const storeDirMode = 0o700

// Passed is whether a pass was recorded under the key.
func (s Store) Passed(key string) (bool, error) {
	path, err := s.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("cannot read the suite pass record: %w", err)
}

// Record writes a pass under the key.
func (s Store) Record(key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, storeDirMode); err != nil {
		return fmt.Errorf("cannot make the suite pass record directory: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("cannot record the suite pass: %w", err)
	}
	return nil
}

// path is the record file of a key; a key that is not a SHA-256 hex digest is refused, so it can
// never name a path outside the store.
func (s Store) path(key string) (string, error) {
	if raw, err := hex.DecodeString(key); err != nil || len(raw) != sha256.Size {
		return "", fmt.Errorf("suite pass key %q is not a SHA-256 hex digest", key)
	}
	return filepath.Join(s.Dir, key), nil
}

// storeName is the store's directory under a cache directory.
const storeName = "fedora-desktop-qa/suite-passes"

// Dir is where the store lives: QA_SUITE_PASSES, else $XDG_CACHE_HOME, else $HOME/.cache, each
// with storeName under it but the first. ok is false when none is set: there is no store, and
// every suite runs.
func Dir(getenv func(string) string) (dir string, ok bool) {
	if d := getenv("QA_SUITE_PASSES"); d != "" {
		return d, true
	}
	if d := getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, storeName), true
	}
	if d := getenv("HOME"); d != "" {
		return filepath.Join(d, ".cache", storeName), true
	}
	return "", false
}
