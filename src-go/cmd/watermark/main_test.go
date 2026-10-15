package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown option is exit 2 before anything runs (M1).
	if code := run([]string{"--frobnicate"}); code != 2 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}
