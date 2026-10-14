package main

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/all"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(all.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(all.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A scripts directory with no gate table is a run that could not start.
	if code := run([]string{"--root", t.TempDir(), "--scripts", t.TempDir()}); code != int(all.ExitCannotRun) {
		t.Fatalf("empty scripts directory: exit %d", code)
	}
}

func TestEachRunGetsItsOwnCountsToken(t *testing.T) {
	first, second := newToken(), newToken()
	if first == second || !strings.HasPrefix(first, "qa-all-") {
		t.Fatalf("tokens %q and %q", first, second)
	}
}
