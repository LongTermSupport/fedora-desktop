package badrecordfile

import (
	"path/filepath"
	"testing"

	reg "fix/internal/registry"
)

// A test plants a record where the writer would: not judged.
func TestPlant(t *testing.T) {
	store := &reg.Store{}
	if filepath.Join(store.Dir(), "name") == "" {
		t.Fatal("no path")
	}
	if err := store.Remove("name"); err != nil {
		t.Fatal(err)
	}
}
