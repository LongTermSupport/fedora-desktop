package reclaim

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// bigFile is one regular file and its size.
type bigFile struct {
	path string
	size int64
}

// found is what largestFiles saw: the files, largest first, and the entries it could not read.
type found struct {
	files      []bigFile
	unreadable int
	first      string
}

// largestFiles is E18's walk: the n largest regular files under dir (equal sizes by path),
// never following a symlink or leaving dir's filesystem.
func largestFiles(dir string, n int) (found, error) {
	root, err := os.Lstat(dir)
	if err != nil {
		return found{}, fmt.Errorf("lstat: %w", err)
	}
	rootDev, ok := device(root)
	if !ok {
		return found{}, errors.New("its device cannot be read")
	}
	f := found{}
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			f.note(p, walkErr)
			return nil
		}
		if d.IsDir() {
			return f.enter(p, d, p == dir, rootDev)
		}
		if d.Type().IsRegular() {
			f.add(p, d, n)
		}
		return nil
	})
	if walkErr != nil {
		return found{}, fmt.Errorf("walk: %w", walkErr)
	}
	f.keep(n)
	return f, nil
}

// enter is whether the walk goes into directory p: not when it is on another filesystem.
func (f *found) enter(p string, d fs.DirEntry, top bool, rootDev uint64) error {
	if top {
		return nil
	}
	info, err := d.Info()
	if err != nil {
		f.note(p, err)
		return filepath.SkipDir
	}
	if dev, ok := device(info); ok && dev != rootDev {
		return filepath.SkipDir
	}
	return nil
}

// add takes regular file p, trimming the list now and then so it stays short.
func (f *found) add(p string, d fs.DirEntry, n int) {
	info, err := d.Info()
	if err != nil {
		f.note(p, err)
		return
	}
	f.files = append(f.files, bigFile{path: p, size: info.Size()})
	if len(f.files) > 4*n+64 {
		f.keep(n)
	}
}

func device(info fs.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Dev, true
}

// keep sorts the files largest first, equal sizes by path, and keeps the first n.
func (f *found) keep(n int) {
	slices.SortFunc(f.files, func(a, b bigFile) int {
		if a.size != b.size {
			if a.size > b.size {
				return -1
			}
			return 1
		}
		return strings.Compare(a.path, b.path)
	})
	f.files = f.files[:min(n, len(f.files))]
}

// note counts an entry that could not be read, naming the first.
func (f *found) note(p string, err error) {
	f.unreadable++
	if f.first != "" {
		return
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		f.first = pe.Path + ": " + pe.Err.Error()
		return
	}
	f.first = p + ": " + err.Error()
}

// clearInside removes every entry inside each dir, keeping the dirs themselves (E24, E26). A
// dir that does not exist has nothing to remove; every entry that cannot be removed is named.
// A cancel stops it before the next entry, saying how far it got (E30); what was is named
// for the person by what.
func (r *run) clearInside(ctx context.Context, what string, dirs ...string) bool {
	var all []string
	done := true
	for _, dir := range dirs {
		des, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			r.failf("could not read %s: %v", dir, err)
			done = false
			continue
		}
		for _, d := range des {
			all = append(all, filepath.Join(dir, d.Name()))
		}
	}
	for i, p := range all {
		if ctx.Err() != nil {
			r.errLine(fmt.Sprintf("  %s is only partly cleared: cancelled after %d of %d entries.", capital(what), i, len(all)))
			return false
		}
		if err := r.env.RemoveAll(p); err != nil {
			r.failf("could not remove %s: %v", p, err)
			done = false
		}
	}
	if !done {
		r.errLine("  Some items in " + what + " could not be removed.")
	}
	return done
}

// capital is s with its first letter upper case, for a name that starts a sentence.
func capital(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
