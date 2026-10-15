package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/deployeddrift"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	argv := append([]string{"git", "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: argv, Dir: dir})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %+v", args, err, res)
	}
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(deployeddrift.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(deployeddrift.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root with no source directory is a run that could not complete.
	if code := run([]string{"--root", t.TempDir()}); code != int(deployeddrift.ExitCannotRun) {
		t.Fatalf("empty root: exit %d", code)
	}
}

func TestRootThatDoesNotExistIsExitTwo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	if code := run([]string{"--root", missing}); code != int(deployeddrift.ExitCannotRun) {
		t.Fatalf("missing root: exit %d", code)
	}
}

func TestLinkedWorktreeTellsMainCheckoutFromLinkedWorktree(t *testing.T) {
	main := t.TempDir()
	git(t, main, "init", "-q")
	git(t, main, "commit", "-q", "--allow-empty", "-m", "init")
	linked := filepath.Join(t.TempDir(), "wt")
	git(t, main, "worktree", "add", "-q", "-b", "side", linked)

	for dir, want := range map[string]bool{main: false, linked: true} {
		got, err := linkedWorktree(dir)
		if err != nil || got != want {
			t.Errorf("linkedWorktree(%s) = %v, %v; want %v", dir, got, err, want)
		}
	}
}

func TestLinkedWorktreeOutsideAGitCheckoutIsAnError(t *testing.T) {
	if _, err := linkedWorktree(t.TempDir()); err == nil {
		t.Fatal("a directory that is not a git checkout was answered as not linked")
	}
}
