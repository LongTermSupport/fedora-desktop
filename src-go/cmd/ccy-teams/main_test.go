package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}, func(string) string { return "" }); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}, func(string) string { return "" }); code != term.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
}
