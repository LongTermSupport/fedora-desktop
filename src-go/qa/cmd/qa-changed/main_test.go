package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/changed"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(changed.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(changed.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A scripts directory with no tables is a run that could not start.
	if code := run([]string{"--root", t.TempDir(), "--scripts", t.TempDir()}); code != int(changed.ExitCannotRun) {
		t.Fatalf("empty scripts directory: exit %d", code)
	}
}

func TestLookPathFindsAToolAndRefusesAMissingOne(t *testing.T) {
	if err := lookPath("sh"); err != nil {
		t.Fatalf("sh: %v", err)
	}
	if err := lookPath("no-such-tool-for-qa-changed"); err == nil {
		t.Fatal("a missing tool was found")
	}
}
