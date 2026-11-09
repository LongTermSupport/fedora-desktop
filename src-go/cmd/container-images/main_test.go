package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--frobnicate"}); code != term.ExitUsage {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}

func TestRunWithNoHomeFails(t *testing.T) {
	t.Setenv("HOME", "")
	if code := run([]string{"prune"}); code != term.ExitFailure {
		t.Fatalf("exit %d with no HOME", code)
	}
}
