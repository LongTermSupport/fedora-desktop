package elsewhere

import (
	"os"
	"testing"

	"fix/internal/teamchat/paths"
)

// Another package's tests have no turn-end seam and are not judged.
func TestElsewhere(t *testing.T) {
	var r paths.Run
	if _, err := os.ReadFile(r.KeeperLog()); err == nil {
		t.Log("read")
	}
}
