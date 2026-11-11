package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ciscope"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the scope, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(ciscope.ExitCannotRun))
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

// The real git over a clone whose work branch is built on origin/F43: the scope is diff mode
// against F43's tip, and a branch built on no line is refused.
func TestRealGitPicksTheLineTheBranchIsBuiltOn(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "F44")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	git(t, repo, "branch", "F43")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "F44 moves on")
	git(t, repo, "update-ref", "refs/remotes/origin/F44", "F44")
	git(t, repo, "switch", "-q", "F43")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "F43 moves on")
	git(t, repo, "update-ref", "refs/remotes/origin/F43", "F43")
	f43 := git(t, repo, "rev-parse", "F43")
	git(t, repo, "switch", "-q", "-c", "ci/topic")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy := "cap = 10\nwarn_days = 3\nfail_days = 7\nexempt = [\"F44\", \"F43\", \"F44-arm\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "scripts", "branch-policy.toml"), []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runIn([]string{"--event", "push", "--ref", "refs/heads/ci/topic", "--default", "F44"}, &out, &errOut, repo)
	if code != int(ciscope.ExitOK) || out.String() != "mode=diff\nline=F43\nbase="+f43+"\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	git(t, repo, "switch", "-q", "--orphan", "lost")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "unrelated")
	out.Reset()
	code = runIn([]string{"--event", "push", "--ref", "refs/heads/lost", "--default", "F44"}, &out, &errOut, repo)
	if code != int(ciscope.ExitRefused) || out.String() != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}
