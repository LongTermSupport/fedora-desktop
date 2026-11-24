// Package goodrecordfile uses the file the reading carries.
package goodrecordfile

import (
	"path/filepath"

	"fix/internal/otherstore"
	"fix/internal/registry"
)

// forget removes the file each record was read from.
func forget(store *registry.Store, name string) error {
	reading, err := store.ReadAll()
	if err != nil {
		return err
	}
	for i, rec := range reading.Records {
		if rec.Name == name {
			if err := store.RemoveFile(reading.Files[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// said names the directory alone, its parent, and itself through a one-element join.
func said(store *registry.Store) []string {
	d := store.Dir()
	return []string{d, filepath.Dir(d) + "/last-restore", filepath.Join(d), "registry " + d}
}

// elsewhere joins a directory that is not the registry's, and removes through another Store.
func elsewhere(o otherstore.Store, dir, name string) (string, error) {
	return filepath.Join(dir, name) + filepath.Join(o.Dir(), name), o.Remove(name)
}

// written is where the writer put the record.
func written(store *registry.Store, rec registry.Record) string {
	return store.Write(rec)
}
