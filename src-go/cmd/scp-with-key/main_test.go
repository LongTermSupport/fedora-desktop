package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// The key flag with no value is a usage error before anything runs (SPEC-ssh-with-key E3).
	if code := run([]string{"-i"}); code != 64 {
		t.Fatalf("-i: exit %d", code)
	}
}
