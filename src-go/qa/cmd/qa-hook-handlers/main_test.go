package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/hookhandlers"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(hookhandlers.ExitCannotRun))
}

// The command's directory test is the real file system's.
func TestIsDirAsksTheFileSystem(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !isDir(dir) || isDir(file) || isDir(filepath.Join(dir, "missing")) {
		t.Fatalf("isDir(%s)=%v isDir(file)=%v", dir, isDir(dir), isDir(file))
	}
}

// --help runs through the real environment and answers without starting anything.
func TestHelpRuns(t *testing.T) {
	if got := run([]string{"--help"}); got != int(hookhandlers.ExitOK) {
		t.Fatalf("exit %d", got)
	}
}
