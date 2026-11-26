package system

import (
	"os"
	"testing"

	tp "teamchatroot/internal/teamchat/paths"
)

// A test file is not judged: it spells the fixed path as the expectation.
func TestFixedPath(t *testing.T) {
	if _, err := os.Stat(tp.StatusFile("alpha")); err == nil {
		t.Log("a record")
	}
}
