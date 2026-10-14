package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	// A usage mistake exits 1 (the bash tool's status, kept at cut-over), before anything runs.
	if code := run([]string{"--nope"}); code != term.ExitFailure {
		t.Fatalf("--nope: exit %d", code)
	}
}
