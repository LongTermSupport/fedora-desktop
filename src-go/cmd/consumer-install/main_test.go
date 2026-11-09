package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Through the command: help is exit 0, an unknown argument a usage error.
func TestRun(t *testing.T) {
	if code := run([]string{"--help"}); code != term.ExitOK {
		t.Errorf("--help exit %d", code)
	}
	if code := run([]string{"extra"}); code != term.ExitUsage {
		t.Errorf("extra exit %d", code)
	}
}
