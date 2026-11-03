package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/untrackedaudit"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(untrackedaudit.ExitCannotRun))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: append([]string{"git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...),
		Dir:  dir,
	})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %+v", args, err, res)
	}
}

func write(t *testing.T, root, rel, text string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// old is a fixed instant years before any run: older than the floor, with no wall clock read.
var old = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

const realPolicy = "ageHours: 24\nstores: [repos]\ndaemonStores: [agent-reports]\nassetExtensions: [.md, .patch, .diff]\nscratchShapes: [\"msg-*\"]\nactiveBranches: []\n"

// A real repository with a remote: clean, then an old note, an unpushed branch and a stash each
// turn it red.
func TestEndToEndOnARealRepository(t *testing.T) {
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "scripts/untracked-policy.yml", realPolicy)
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "init")
	git(t, root, "remote", "add", "origin", remote)
	git(t, root, "push", "-q", "origin", "main")
	git(t, root, "fetch", "-q", "origin")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitOK) {
		t.Fatalf("clean repository: exit %d", code)
	}

	note := write(t, root, "untracked/scratch/research.md", "findings")
	fresh := write(t, root, "untracked/scratch/msg-1", "body")
	if err := os.Chtimes(note, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, old, old); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitFindings) {
		t.Fatalf("old note: exit %d", code)
	}
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitOK) {
		t.Fatalf("old allowlisted body only: exit %d", code)
	}

	git(t, root, "checkout", "-q", "-b", "feat/local")
	write(t, root, "work.txt", "x")
	git(t, root, "add", "work.txt")
	git(t, root, "commit", "-q", "-m", "local only")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitFindings) {
		t.Fatalf("unpushed branch: exit %d", code)
	}
	git(t, root, "push", "-q", "origin", "feat/local")
	git(t, root, "fetch", "-q", "origin")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitOK) {
		t.Fatalf("pushed branch: exit %d", code)
	}

	write(t, root, "work.txt", "changed")
	git(t, root, "stash", "push", "-q")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitFindings) {
		t.Fatalf("stash: exit %d", code)
	}
}

func TestOldDirtyLinkedWorktreeIsFound(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "scripts/untracked-policy.yml", realPolicy)
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "init")
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	git(t, root, "remote", "add", "origin", remote)
	git(t, root, "push", "-q", "origin", "main")
	git(t, root, "fetch", "-q", "origin")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, root, "worktree", "add", "-q", "-b", "side", wt, "main")
	git(t, root, "push", "-q", "origin", "side")
	git(t, root, "fetch", "-q", "origin")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitOK) {
		t.Fatalf("clean worktree: exit %d", code)
	}
	dirty := write(t, wt, "scratch.txt", "x")
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitOK) {
		t.Fatalf("fresh dirty worktree: exit %d", code)
	}
	if err := os.Chtimes(dirty, old, old); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitFindings) {
		t.Fatalf("old dirty worktree: exit %d", code)
	}
}

func TestNotARepositoryCannotBeJudged(t *testing.T) {
	root := t.TempDir()
	write(t, root, "scripts/untracked-policy.yml", realPolicy)
	if code := run([]string{"--root", root}); code != int(untrackedaudit.ExitCannotRun) {
		t.Fatalf("exit %d", code)
	}
}
