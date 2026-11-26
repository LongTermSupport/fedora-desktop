package front

import (
	"os"
	"path/filepath"
	"testing"
)

// A test plants the files the refusal is proven on: not judged.
func TestPlant(t *testing.T) {
	if err := os.WriteFile(filepath.Join(t.TempDir(), ".claude/ccy/mounts"), nil, 0o600); err == nil {
		t.Log("planted")
	}
}
