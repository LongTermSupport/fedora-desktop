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
	// Any other argument is a usage error before anything runs (V10).
	if code := run([]string{"--pause"}, signals); code != 64 {
		t.Fatalf("--pause: exit %d", code)
	}
}
