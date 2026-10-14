package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bashlimits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != term.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root with no git repository and no allowlist is a gate that could not run.
	if code := run([]string{"--root", t.TempDir()}); code != bashlimits.ExitCannotRun {
		t.Fatalf("empty root: exit %d", code)
	}
}
