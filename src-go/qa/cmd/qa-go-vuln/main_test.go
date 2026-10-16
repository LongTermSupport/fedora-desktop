package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/godeps"
)

// The scan itself needs the vulnerability database, so the command's own test stops at its
// grammar; godeps' tests drive the scan through a fake go, and the gate runs it on the real tree.
func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(godeps.ExitOK) {
		t.Fatalf("--help: code %d", code)
	}
	if code := run([]string{"--root"}); code != int(godeps.ExitCannotRun) {
		t.Fatalf("--root with no value: code %d", code)
	}
}
