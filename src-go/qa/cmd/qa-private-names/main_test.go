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

// The pinned list is replaced whole and owner-only, even over a file a looser mode left (os.Create
// is 0666 less the umask), and nothing of the write is left beside it.
func TestReplacePrivateIsOwnerOnlyAndWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pinned")
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := replacePrivate(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil || string(data) != "new\n" {
		t.Fatalf("content %q %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the directory holds %d entries (%v), want the list alone", len(entries), err)
	}
	if err := replacePrivate(filepath.Join(dir, "missing", "pinned"), []byte("x\n")); err == nil {
		t.Fatal("a write into a missing directory succeeded")
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
