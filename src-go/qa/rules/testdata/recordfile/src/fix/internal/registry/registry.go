// Package registry stands in for internal/registry: the store of session records, its writer
// (which names a record's file after the record, and is not judged) and its reading.
package registry

import "path/filepath"

// Record is a session record.
type Record struct {
	Name string
}

// Reading is every record, with the file each was read from.
type Reading struct {
	Records []Record
	Files   []string
}

// Store is a registry directory.
type Store struct {
	dir string
}

// Dir is the registry directory of the environment.
func Dir(getenv func(string) string) (string, error) {
	return getenv("CCY_STATE_DIR") + "/sessions", nil
}

// Dir is the directory holding the records.
func (s *Store) Dir() string { return s.dir }

// Write is the writer: it names the file after the record.
func (s *Store) Write(r Record) string { return filepath.Join(s.dir, r.Name) }

// Remove is the name-keyed removal.
func (s *Store) Remove(name string) error {
	_ = filepath.Join(s.Dir(), name)
	return nil
}

// RemoveFile removes the file a reading carries.
func (s *Store) RemoveFile(_ string) error { return nil }

// New is the store in dir.
func New(dir string) *Store { return &Store{dir: dir} }

// ReadAll is the reading.
func (s *Store) ReadAll() (Reading, error) { return Reading{}, nil }
