// Package badwalkentry treats a listed entry as a directory without asking its type, every way
// the rule sees.
package badwalkentry

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"fix/internal/store"
)

// known is the defect of fedora-desktop#404: every entry of control/ is taken for a session
// folder, so a regular file beside them is read as one.
func known(st store.Store, control string) (bool, error) {
	entries, err := os.ReadDir(control)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		session, err := store.Parse(e.Name())
		if err != nil {
			return false, err
		}
		file := st.DesiredFile(session)
		if _, err := os.Lstat(file); err != nil { // want `a path beneath an entry a directory listing found`
			return false, err
		}
	}
	return false, nil
}

// joined reads a file inside each entry, joined in one call.
func joined(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := os.ReadFile(filepath.Join(dir, e.Name(), "meta.json")); err != nil { // want `a path beneath an entry a directory listing found`
			return err
		}
	}
	return nil
}

// listed lists each entry as a directory.
func listed(dir string) (int, error) {
	n := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		sub, err := os.ReadDir(filepath.Join(dir, e.Name())) // want `a path beneath an entry a directory listing found`
		if err != nil {
			return 0, err
		}
		n += len(sub)
	}
	return n, nil
}

// relayed holds the entry's path in a local and opens a file under it, from a listing of an
// open directory.
func relayed(d *os.File, dir string) error {
	entries, err := d.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		f, err := os.Open(filepath.Join(p, "lock")) // want `a path beneath an entry a directory listing found`
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// infos ranges over Readdir's FileInfo and reads under each through an fs.FS.
func infos(d *os.File, fsys fs.FS) error {
	list, err := d.Readdir(-1)
	if err != nil {
		return err
	}
	for _, info := range list {
		if _, err := fs.ReadFile(fsys, path.Join(info.Name(), "x")); err != nil { // want `a path beneath an entry a directory listing found`
			return err
		}
	}
	return nil
}

// lister is a listing returned by a function.
func lister(dir string) []fs.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

// concatenated builds the path with +.
func concatenated(dir string) error {
	for _, e := range lister(dir) {
		if _, err := os.Stat(dir + "/" + e.Name() + "/x"); err != nil { // want `a path beneath an entry a directory listing found`
			return err
		}
	}
	return nil
}
