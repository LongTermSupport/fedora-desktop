package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown argument is a usage error, exit 2, before anything runs (E2).
	if code := run([]string{"--frobnicate"}); code != 2 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}
