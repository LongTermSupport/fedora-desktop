package tarstate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

const (
	extractedDirMode  = 0o700
	extractedFileMode = 0o600
)

// Extract writes the validated state into stage, which must be an existing, empty
// directory (not a symlink). Everything is created through an os.Root on stage, so no
// name can leave it, and files are created exclusively, so nothing is overwritten or
// followed. Directories are 0700 and files 0600 whatever the tar said; owner, when not
// nil, owns stage and everything in it.
func (s *State) Extract(stage string, owner *fsx.Owner) error {
	if err := checkStage(stage); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Clean(stage))
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	extractErr := s.extractInto(root, stage, owner)
	if closeErr := root.Close(); closeErr != nil {
		return errors.Join(extractErr, fmt.Errorf("extract: closing %s: %w", stage, closeErr))
	}
	return extractErr
}

func (s *State) extractInto(root *os.Root, stage string, owner *fsx.Owner) error {
	if err := requireEmpty(root, stage); err != nil {
		return err
	}
	for _, e := range s.entries {
		if e.name == "." || !fs.ValidPath(e.name) {
			return fmt.Errorf("extract: %q is not a path inside the stage", e.name)
		}
	}
	if err := s.makeDirs(root, owner); err != nil {
		return err
	}
	for _, e := range s.entries {
		if e.dir {
			continue
		}
		if err := writeFile(root, e, owner); err != nil {
			return err
		}
	}
	return nil
}

func checkStage(stage string) error {
	info, err := os.Lstat(stage)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("extract: stage %s is a symlink", stage)
	case !info.IsDir():
		return fmt.Errorf("extract: stage %s is not a directory", stage)
	}
	return nil
}

func requireEmpty(root *os.Root, stage string) error {
	dot, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	names, readErr := dot.Readdirnames(1)
	if closeErr := dot.Close(); closeErr != nil {
		return fmt.Errorf("extract: closing %s: %w", stage, closeErr)
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("extract: reading %s: %w", stage, readErr)
	}
	if len(names) > 0 {
		return fmt.Errorf("extract: stage %s is not empty", stage)
	}
	return nil
}

// makeDirs creates every directory the state needs: the explicit ones and the parents of
// every member, shortest path first.
func (s *State) makeDirs(root *os.Root, owner *fsx.Owner) error {
	want := map[string]bool{}
	for _, e := range s.entries {
		if e.dir {
			want[e.name] = true
		}
		for parent := range parents(e.name) {
			want[parent] = true
		}
	}
	dirs := make([]string, 0, len(want))
	for d := range want {
		dirs = append(dirs, d)
	}
	slices.SortFunc(dirs, func(a, b string) int {
		if da, db := strings.Count(a, "/"), strings.Count(b, "/"); da != db {
			return da - db
		}
		return strings.Compare(a, b)
	})
	if err := settle(root, ".", owner); err != nil {
		return err
	}
	for _, d := range dirs {
		if err := root.Mkdir(d, extractedDirMode); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		if err := settle(root, d, owner); err != nil {
			return err
		}
	}
	return nil
}

// settle sets the mode and, when owner is not nil, the ownership of a directory.
func settle(root *os.Root, name string, owner *fsx.Owner) error {
	if owner != nil {
		if err := root.Lchown(name, owner.UID, owner.GID); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
	}
	if err := root.Chmod(name, extractedDirMode); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	return nil
}

func writeFile(root *os.Root, e entry, owner *fsx.Owner) error {
	f, err := root.OpenFile(e.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, extractedFileMode)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	fillErr := fillFile(f, e, owner)
	if closeErr := f.Close(); closeErr != nil {
		return errors.Join(fillErr, fmt.Errorf("extract: closing %s: %w", e.name, closeErr))
	}
	return fillErr
}

func fillFile(f *os.File, e entry, owner *fsx.Owner) error {
	if owner != nil {
		if err := f.Chown(owner.UID, owner.GID); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
	}
	if err := f.Chmod(extractedFileMode); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if _, err := f.Write(e.data); err != nil {
		return fmt.Errorf("extract: writing %s: %w", e.name, err)
	}
	return nil
}
