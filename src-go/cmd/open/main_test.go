package main

import (
	"os"
	"testing"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	sig := make(chan os.Signal, 1)
	if code := run([]string{"--help"}, sig); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown option is a usage error before anything runs (E2).
	if code := run([]string{"--frobnicate"}, sig); code != 64 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}
