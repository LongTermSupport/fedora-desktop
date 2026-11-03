package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUsesTheStateDirectoryUnderHome(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "bash")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(state, "history")
	if err := os.WriteFile(hist, []byte("#1700000001\nkeep\n#1700000002\ndrop-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"--yes", "drop-me"}, home, strings.NewReader(""), false, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got, err := os.ReadFile(filepath.Clean(hist))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#1700000001\nkeep\n" {
		t.Errorf("history = %q", got)
	}
}

func TestRunWithoutHomeIsAnError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"x"}, "", strings.NewReader(""), false, &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}
