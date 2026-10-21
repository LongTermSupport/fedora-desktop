package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The inner status line of the project's settings is printed, with one final newline, and the
// render exits 0 (S2, S3).
func TestRunPrintsTheInnerLineAndExitsZero(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	settings := `{"statusLine": {"type": "command", "command": "echo inner"}}`
	if err := os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"session_id": "sess-1", "workspace": {"project_dir": "` + project + `"}}`
	var stdout, stderr bytes.Buffer
	env := []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
	code := run(strings.NewReader(payload), &stdout, &stderr, env, make(chan os.Signal))
	if code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != "inner\n" {
		t.Fatalf("stdout %q, want %q", stdout.String(), "inner\n")
	}
}
