package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ccyversionbase"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the base, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(ccyversionbase.ExitCannotRun))
}

var gitEnv = []string{
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git", "-C", dir}, args...), Env: gitEnv})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %d\n%s", args, err, res.Code, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

func commit(t *testing.T, dir, file string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", file)
	git(t, dir, "commit", "-q", "-m", file)
	return git(t, dir, "rev-parse", "HEAD")
}

// world is a repository whose origin/F44 and work branch have diverged by one commit each, and
// a second work branch beside them.
type world struct{ repo, work, f44 string }

func newWorld(t *testing.T) world {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "F44")
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy := "cap = 10\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\", \"F43\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "scripts", "branch-policy.toml"), []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "base")
	git(t, repo, "branch", "other")
	git(t, repo, "switch", "-q", "-c", "work")
	w := world{repo: repo, work: commit(t, repo, "work")}
	git(t, repo, "switch", "-q", "F44")
	w.f44 = commit(t, repo, "release")
	git(t, repo, "update-ref", "refs/remotes/origin/F44", "F44")
	git(t, repo, "switch", "-q", "other")
	commit(t, repo, "other")
	return w
}

func base(t *testing.T, dir string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runIn(nil, &out, &errOut, dir)
	return code, out.String(), errOut.String()
}

func wantBase(t *testing.T, dir, want string) {
	t.Helper()
	if code, out, errOut := base(t, dir); code != 0 || out != want+"\n" {
		t.Fatalf("exit %d, stdout %q, want %q; stderr %q", code, out, want, errOut)
	}
}

// The real git: outside a merge the base is HEAD; F44 merged into the work branch, and the work
// branch merged onto F44, are both judged against F44's tip.
func TestRealGitNamesTheProtectedParent(t *testing.T) {
	w := newWorld(t)
	git(t, w.repo, "switch", "-q", "work")
	wantBase(t, w.repo, "HEAD")
	git(t, w.repo, "merge", "--no-commit", "--no-ff", "-q", "origin/F44")
	wantBase(t, w.repo, w.f44)
	git(t, w.repo, "merge", "--abort")
	git(t, w.repo, "switch", "-q", "--detach", "origin/F44")
	git(t, w.repo, "merge", "--no-commit", "--no-ff", "-q", w.work)
	wantBase(t, w.repo, w.f44)
}

// Two work branches merged: no parent is on F44, so no base is owed.
func TestRealGitTwoWorkBranchesHaveNoBase(t *testing.T) {
	w := newWorld(t)
	git(t, w.repo, "merge", "--no-commit", "--no-ff", "-q", "work")
	wantBase(t, w.repo, "")
}

// The real git reads the versions from the index and each parent: a merge that keeps the work
// branch's lower CCY_VERSION is refused, and raising it lets the base through.
func TestRealGitRefusesAMergeBelowAParent(t *testing.T) {
	w := newWorld(t)
	launcher := filepath.Join(w.repo, "files", "var", "local", "claude-yolo", "claude-yolo")
	set := func(v string) {
		if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(launcher, []byte("CCY_VERSION=\""+v+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		git(t, w.repo, "add", launcher)
	}
	git(t, w.repo, "switch", "-q", "F44")
	set("1.0.2")
	git(t, w.repo, "commit", "-q", "-m", "release 1.0.2")
	git(t, w.repo, "update-ref", "refs/remotes/origin/F44", "F44")
	git(t, w.repo, "switch", "-q", "work")
	set("1.0.1")
	git(t, w.repo, "commit", "-q", "-m", "bump 1.0.1")
	git(t, w.repo, "merge", "--no-commit", "--no-ff", "-q", "-s", "ours", "origin/F44")
	if code, out, errOut := base(t, w.repo); code != 1 || out != "" || !strings.Contains(errOut, "CCY_VERSION 1.0.1 in the merge is below 1.0.2") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	set("1.0.3")
	wantBase(t, w.repo, git(t, w.repo, "rev-parse", "origin/F44"))
}
