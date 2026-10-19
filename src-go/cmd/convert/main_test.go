package main

import (
	"context"
	"testing"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run(context.Background(), []string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown option is a usage error before anything runs (SPEC-convert.md E2).
	if code := run(context.Background(), []string{"--frobnicate"}); code != 64 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}
