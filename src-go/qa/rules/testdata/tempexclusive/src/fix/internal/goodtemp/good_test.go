package goodtemp

import (
	"os"
	"testing"
)

// A test's temporary is in its own t.TempDir, which no other user writes.
func TestWrite(t *testing.T) {
	path := t.TempDir() + "/f"
	if err := os.WriteFile(path+".tmp", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}
