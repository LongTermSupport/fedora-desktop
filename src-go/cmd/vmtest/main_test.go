package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown command is a usage error, exit 64, before anything runs (E1).
	if code := run([]string{"frobnicate"}); code != term.ExitUsage {
		t.Fatalf("frobnicate: exit %d", code)
	}
}
