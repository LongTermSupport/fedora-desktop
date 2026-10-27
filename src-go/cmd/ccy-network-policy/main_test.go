package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunWiresTheCommand(t *testing.T) {
	if code := run(nil); code != term.ExitUsage {
		t.Fatalf("no subcommand: exit %d, want 64", code)
	}
	t.Setenv("HOME", t.TempDir())
	if code := run([]string{"where"}); code != term.ExitOK {
		t.Fatalf("where: exit %d, want 0", code)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if code := run([]string{"refresh"}); code != term.ExitOK {
		t.Fatalf("refresh with no live session: exit %d, want 0", code)
	}
}
