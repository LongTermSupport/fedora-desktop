package sessions

import (
	"testing"

	"fix/internal/registry"
)

// A test reads the field to check what was recorded: it is not judged.
func TestRecorded(t *testing.T) {
	if (registry.Record{Launcher: "/x"}).Launcher != "/x" || len(Held(registry.Record{Launcher: "/x"})) == 0 {
		t.Fatal("recorded")
	}
}
