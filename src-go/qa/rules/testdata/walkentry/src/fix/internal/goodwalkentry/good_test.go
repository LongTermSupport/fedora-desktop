package goodwalkentry

import (
	"os"
	"path/filepath"
	"testing"
)

// A test is not judged: it lists a tree it made itself.
func TestTree(t *testing.T) {
	dir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "x")); err != nil {
			t.Fatal(err)
		}
	}
}
