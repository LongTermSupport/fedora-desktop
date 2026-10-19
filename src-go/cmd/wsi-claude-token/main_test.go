package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// Any argument is a usage error before anything runs (K8).
	if code := run([]string{"--debug"}); code != 64 {
		t.Fatalf("--debug: exit %d", code)
	}
	// No HOME: exit 1 before anything runs.
	t.Setenv("HOME", "")
	if code := run(nil); code != 1 {
		t.Fatalf("no HOME: exit %d", code)
	}
}
