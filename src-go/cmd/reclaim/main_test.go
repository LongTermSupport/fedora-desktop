package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown argument is a usage error before anything runs (SPEC-reclaim E2).
	if code := run([]string{"--frobnicate"}); code != 64 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}
