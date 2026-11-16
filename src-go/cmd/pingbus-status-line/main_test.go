package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
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
	code := run(strings.NewReader(payload), &stdout, &stderr, env, fixedCwd(project), make(chan os.Signal), clock.NewFake(0, 0))
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
	if code := run(failingReader{}, &stdout, &stderr, env, noCwd, make(chan os.Signal), clock.NewFake(0, 0)); code != term.ExitOK {
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
	code := run(reader, &stdout, &stderr, nil, fixedCwd("/"), signals, clock.NewFake(0, 0))
	if code != term.ExitCode(128+int(syscall.SIGTERM)) || stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, stdout.String())
	}
}

// sentWriter is a stderr that hands each write to the test.
type sentWriter chan string

func (w sentWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

// Issue 260: a payload that has not come (an open pipe nothing writes to) is said on stderr
// after stdinread.NoticeAfter; the render goes on waiting for it, and a signal still ends it.
func TestRunSaysItIsWaitingForThePayload(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{w, r} {
			if err := f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		}
	})
	f := clock.NewFake(0, 0)
	signals := make(chan os.Signal, 1)
	var stdout bytes.Buffer
	stderr := make(sentWriter, 1)
	done := make(chan term.ExitCode, 1)
	go func() { done <- run(r, &stdout, stderr, nil, fixedCwd("/"), signals, f) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := f.AwaitDue(ctx, stdinread.NoticeAfter); err != nil {
		t.Fatal(err)
	}
	f.Advance(stdinread.NoticeAfter)
	if said := <-stderr; !strings.HasPrefix(said, "pingbus status-line: waiting for input on stdin") {
		t.Fatalf("stderr %q", said)
	}
	signals <- syscall.SIGTERM
	if code := <-done; code != term.ExitCode(128+int(syscall.SIGTERM)) || stdout.Len() != 0 {
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
