package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// No path is a usage error before anything runs (S15).
	if code := run(nil); code != 64 {
		t.Fatalf("no path: exit %d", code)
	}
	// A path that is not there: exit 1 before anything runs.
	if code := run([]string{t.TempDir() + "/gone"}); code != 1 {
		t.Fatalf("missing path: exit %d", code)
	}
}
