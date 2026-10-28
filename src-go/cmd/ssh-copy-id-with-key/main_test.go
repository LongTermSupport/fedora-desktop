package main

import "testing"

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// The key flag with no value is a usage error before anything runs (SPEC-ssh-with-key E3).
	if code := run([]string{"--auth-key"}); code != 64 {
		t.Fatalf("--auth-key: exit %d", code)
	}
}
