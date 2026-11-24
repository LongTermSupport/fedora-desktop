// Package goodwalkentry asks each listed entry's type before using it as a directory, or does
// not use it as one.
package goodwalkentry

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dirsOnly skips every entry that is not a directory.
func dirsOnly(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, e.Name(), "desired.json")); err != nil {
			return err
		}
	}
	return nil
}

// byType switches on the entry's type.
func byType(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		switch {
		case e.Type().IsDir():
			if _, err := os.ReadDir(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		case e.Type().IsRegular():
		default:
			return fs.ErrInvalid
		}
	}
	return nil
}

// followed stats the entry itself, following a symlink, and asks the target's type.
func followed(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		if _, err := os.ReadFile(filepath.Join(p, "meta.json")); err != nil {
			return err
		}
	}
	return nil
}

// fileItself opens the entry itself (project_walk_open's concern, not this rule's) and keeps
// names that pass a suffix filter.
func fileItself(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || !e.Type().IsRegular() {
			continue
		}
		if _, err := os.ReadFile(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// namesOnly reads nothing under the entries.
func namesOnly(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// notAListing ranges over strings, which carry no type to ask.
func notAListing(dir string, names []string) error {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n, "x")); err != nil {
			return err
		}
	}
	return nil
}
