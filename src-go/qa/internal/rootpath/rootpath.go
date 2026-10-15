// Package rootpath is the one place a QA gate turns its --root and --scripts into the
// repository root it works under. The root is absolute, clean and free of symlinks: an engine
// that prints a file's real path (node, ruff, ansible) can then have the root stripped from what
// it prints, and no checkout path reaches stdout or a report. A root that cannot be resolved is
// an error, which each gate reports as exit 2.
package rootpath

import (
	"fmt"
	"path/filepath"
)

// Eval makes a path free of symlinks; filepath.EvalSymlinks is the real one. A gate whose tests
// run on in-memory trees passes its own.
type Eval func(path string) (string, error)

// Resolve makes path absolute against cwd, cleans it and resolves every symlink in it.
func Resolve(cwd, path string) (string, error) {
	return ResolveVia(filepath.EvalSymlinks, cwd, path)
}

// ResolveVia is Resolve with the symlink resolution given.
func ResolveVia(eval Eval, cwd, path string) (string, error) {
	path = absolute(cwd, path)
	resolved, err := eval(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the repository root %s: %w", path, err)
	}
	return filepath.Clean(resolved), nil
}

// Root is the repository root a gate was given: root, else the parent of scripts, else cwd,
// resolved by Resolve.
func Root(cwd, root, scripts string) (string, error) {
	return RootVia(filepath.EvalSymlinks, cwd, root, scripts)
}

// RootVia is Root with the symlink resolution given.
func RootVia(eval Eval, cwd, root, scripts string) (string, error) {
	switch {
	case root != "":
	case scripts != "":
		root = filepath.Dir(absolute(cwd, scripts))
	default:
		root = cwd
	}
	return ResolveVia(eval, cwd, root)
}

// absolute makes path absolute against cwd and cleans it.
func absolute(cwd, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}
