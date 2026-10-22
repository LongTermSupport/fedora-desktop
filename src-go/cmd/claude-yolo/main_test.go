package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The test binary has no bash launcher beside it, so every run is a hand-over that cannot
// exec: exit 1, never a silent success.
func TestRunHandsOverToTheBashLauncherBesideIt(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--token", "work"}, nil} {
		if code := run(args, "1.2.3"); code != term.ExitFailure {
			t.Fatalf("%q: exit %d, want 1 (no claude-yolo.bash beside the test binary)", args, code)
		}
	}
}
