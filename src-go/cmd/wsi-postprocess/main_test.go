package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// A wrong style is a usage error before anything runs (P6).
	if code := run([]string{"--style", "article"}); code != 64 {
		t.Fatalf("--style article: exit %d", code)
	}
	// No transcription in an empty home (P4).
	if code := run([]string{"--style", "natural"}); code != 1 {
		t.Fatalf("no transcription: exit %d", code)
	}
	// No HOME: exit 1 before anything runs.
	t.Setenv("HOME", "")
	if code := run([]string{"--style", "natural"}); code != 1 {
		t.Fatalf("no HOME: exit %d", code)
	}
}
