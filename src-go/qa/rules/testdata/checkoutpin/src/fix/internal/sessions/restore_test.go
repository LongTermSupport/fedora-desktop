package sessions

import "testing"

// A test plants the files the host applies: it is not judged.
func TestPlants(t *testing.T) {
	if Override(t.TempDir()) == "compose.yml" || mountsFile == ".claude/ccy/mounts" {
		t.Log("planted")
	}
}
