package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func fixedCwd(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

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
	code := run(strings.NewReader(payload), &stdout, &stderr, env, fixedCwd(project), make(chan os.Signal))
	if code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != "inner\n" {
		t.Fatalf("stdout %q, want %q", stdout.String(), "inner\n")
	}
}

// failingReader is a stdin whose read fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

// D3: a stdin that cannot be read and a working directory that cannot be had still render, and
// exit 0.
func TestRunRendersWithoutStdinOrWorkingDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	noCwd := func() (string, error) { return "", errors.New("gone") }
	env := []string{"HOME=" + t.TempDir(), "PINGBUS_STATUS_LINE_INNER=1"}
	if code := run(failingReader{}, &stdout, &stderr, env, noCwd, make(chan os.Signal)); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != "\n" {
		t.Errorf("stdout %q", stdout.String())
	}
}

// D3, S12: a signal while stdin is still open ends the render with 128 plus the signal and
// nothing on stdout.
func TestRunCancelledWhileReadingStdin(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	var stdout, stderr bytes.Buffer
	code := run(reader, &stdout, &stderr, nil, fixedCwd("/"), signals)
	if code != term.ExitCode(128+int(syscall.SIGTERM)) || stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, stdout.String())
	}
}

// D4: the first of a repeated name wins, as in Python's os.environ; an entry with no = is skipped.
func TestEnvMapKeepsTheFirstOfARepeatedName(t *testing.T) {
	env := envMap([]string{"A=1", "B", "A=2", "C=x=y"})
	if len(env) != 2 || env["A"] != "1" || env["C"] != "x=y" {
		t.Fatalf("%v", env)
	}
}
