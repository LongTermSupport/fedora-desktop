package goodroot

import (
	"os"
	"testing"
)

// A test writes in its own t.TempDir, which no other user writes.
func TestWrite(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("f", nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
