package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunAnswersThroughTheFront(t *testing.T) {
	if code := run(nil); code != term.ExitUsage {
		t.Fatalf("no file: exit %d, want 64", code)
	}
	// A token outside any pool is not read, so the launch may go on.
	file := filepath.Join(t.TempDir(), "work.2030-06-01.token")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCY_TOKEN_USAGE", "0")
	if code := run([]string{"--", file}); code != term.ExitOK {
		t.Fatalf("exit %d, want 0", code)
	}
}
