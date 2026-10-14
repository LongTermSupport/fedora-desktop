package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gobuild"
)

func TestRunDispatchesThroughGobuild(t *testing.T) {
	if code := run([]string{"nope"}); code != gobuild.ExitCannotRun {
		t.Fatalf("nope: exit %d", code)
	}
	if code := run([]string{"digest", "--source", t.TempDir(), "--command", "x"}); code != gobuild.ExitFailure {
		t.Fatalf("a checkout with no module: exit %d", code)
	}
}
