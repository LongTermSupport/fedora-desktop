package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/pushgate"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(pushgate.ExitCannotRun))
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

// noRuns is GitHub through gh with no run of any commit; it records what it was asked.
type noRuns struct{ asked []string }

func (n *noRuns) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	n.asked = append(n.asked, strings.Join(c.Argv, " "))
	return proc.Result{Stdout: `{"totalCount":0,"runs":[]}`}, nil
}

// The flow's last push, a commit already on the remote's ci/ branch pushed to F44, reaches the
// gate as a ref line that moves F44, and the gate asks CI about it although the remote holds
// every commit of it.
func TestACommitAlreadyOnACIBranchIsStillJudgedForF44(t *testing.T) {
	scripts := hookUnderTest(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, t.TempDir(), "init", "-q", "--bare", remote)
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "F44")
	git(t, repo, "remote", "add", "origin", remote)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "published")
	git(t, repo, "push", "-q", "origin", "F44")
	published := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	work := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "config", "core.hooksPath", filepath.Join(scripts, "git-hooks"))
	git(t, repo, "push", "-q", "origin", "HEAD:refs/heads/ci/topic")
	git(t, repo, "push", "-q", "origin", work+":F44")
	line := read(t, filepath.Join(scripts, "qa-push-gate.stdin"))
	if want := work + " refs/heads/F44 " + published + "\n"; !strings.HasSuffix(line, want) {
		t.Fatalf("the F44 push reached the gate as %q, want a line ending %q", line, want)
	}
	gh := &noRuns{}
	var out, errOut bytes.Buffer
	code := pushgate.Main(context.Background(), []string{"origin", "git@github.com:o/r.git"}, pushgate.Env{
		Stdin: strings.NewReader(line), Stdout: &out, Stderr: &errOut, Runner: gh, Cwd: "../../../..",
		Getenv: func(string) string { return "" }, ReadFile: os.ReadFile,
	})
	if code != pushgate.ExitRefused || len(gh.asked) != 1 || !strings.Contains(gh.asked[0], "head_sha="+work) {
		t.Fatalf("exit %d, asked %v: %s%s", code, gh.asked, out.String(), errOut.String())
	}
}
