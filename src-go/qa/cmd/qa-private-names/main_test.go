package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/privatenames"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(privatenames.ExitCannotRun))
}

// The denylist holds the terms, so it is written for its owner alone, and never over a file.
func TestCreatePrivateIsOwnerOnlyAndRefusesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "denylist")
	if err := createPrivate(path, []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", info.Mode().Perm())
	}
	if again := createPrivate(path, []byte("y\n")); !errors.Is(again, fs.ErrExist) {
		t.Fatalf("an existing file was not refused: %v", again)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil || string(data) != "x\n" {
		t.Fatalf("the existing file was changed: %q %v", data, err)
	}
}

func TestContainerIsTheWorkspaceCheckout(t *testing.T) {
	for cwd, want := range map[string]bool{
		"/workspace": true, "/workspace/.claude/worktrees/x": true, "/workspacey": false, "/home/u/repo": false, "": false,
	} {
		if got := inContainer(cwd); got != want {
			t.Errorf("inContainer(%q) = %t, want %t", cwd, got, want)
		}
	}
}
