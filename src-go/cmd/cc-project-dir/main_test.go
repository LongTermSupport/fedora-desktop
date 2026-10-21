package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}, "/checkout"); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}, "/checkout"); code != term.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
	if code := run(nil, ""); code != term.ExitFailure {
		t.Fatalf("no RootDir: exit %d", code)
	}
}
