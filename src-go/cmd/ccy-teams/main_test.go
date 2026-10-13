package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}, func(string) string { return "" }); code != teams.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}, func(string) string { return "" }); code != teams.ExitUsage {
		t.Fatalf("--nope: exit %d", code)
	}
}
