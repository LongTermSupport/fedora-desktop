package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--version"}); code != 0 {
		t.Fatalf("--version: exit %d", code)
	}
	// An unknown command is a usage error before anything runs (SPEC-nord E2).
	if code := run([]string{"frobnicate"}); code != 64 {
		t.Fatalf("frobnicate: exit %d", code)
	}
}
