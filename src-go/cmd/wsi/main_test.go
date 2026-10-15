package main

import (
	"os"
	"testing"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	signals := make(chan os.Signal, 1)
	if code := run([]string{"--help"}, signals); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// No HOME is exit 1 before anything runs (B18).
	t.Setenv("HOME", "")
	if code := run([]string{"--no-notify"}, signals); code != 1 {
		t.Fatalf("no HOME: exit %d", code)
	}
}
