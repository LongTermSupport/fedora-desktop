package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
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

func git(t *testing.T, dir string, extra []string, args ...string) string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git", "-C", dir}, args...), Env: append(append([]string{}, gitEnv...), extra...)})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %d\n%s", args, err, res.Code, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

func commitAt(t *testing.T, dir string, when time.Time, msg string) string {
	t.Helper()
	stamp := "GIT_COMMITTER_DATE=" + when.Format(time.RFC3339)
	git(t, dir, []string{stamp, "GIT_AUTHOR_DATE=" + when.Format(time.RFC3339)}, "commit", "-q", "--allow-empty", "-m", msg)
	return git(t, dir, nil, "rev-parse", "HEAD")
}

// The gate over a real repository and remote: what git's rev-list, merge-base and log answer is
// what the unit tests' fake assumes.
func TestTheGateOverRealGit(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, t.TempDir(), nil, "init", "-q", "--bare", remote)
	repo := t.TempDir()
	git(t, repo, nil, "init", "-q", "-b", "F44")
	git(t, repo, nil, "remote", "add", "origin", remote)
	published := commitAt(t, repo, base, "published")
	git(t, repo, nil, "push", "-q", "origin", "F44")
	judged := commitAt(t, repo, base.Add(time.Minute), "judged")
	inBurst := commitAt(t, repo, base.Add(5*time.Minute), "made while qa-all ran")
	common := git(t, repo, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	verdictPath := filepath.Join(common, qaverdict.FileName)
	gate := func(tip string) (pushgate.ExitCode, string) {
		var out, errOut bytes.Buffer
		code := pushgate.Main(context.Background(), []string{"origin", remote}, pushgate.Env{
			Stdin:  strings.NewReader("refs/heads/F44 " + tip + " refs/heads/F44 " + published + "\n"),
			Stdout: &out, Stderr: &errOut, Runner: proc.OS{}, Cwd: repo,
			Getenv: func(string) string { return "" },
		})
		return code, out.String() + errOut.String()
	}
	if code, text := gate(inBurst); code != pushgate.ExitRefused {
		t.Fatalf("no verdict: exit %d: %s", code, text)
	}
	write := func(status string) {
		t.Helper()
		rec := qaverdict.Record{Status: status, Commit: judged, Started: base.Add(2 * time.Minute), Finished: base.Add(10 * time.Minute)}
		if err := qaverdict.Write(verdictPath, rec); err != nil {
			t.Fatal(err)
		}
	}
	write(qaverdict.StatusFail)
	if code, text := gate(inBurst); code != pushgate.ExitRefused {
		t.Fatalf("failed verdict: exit %d: %s", code, text)
	}
	write(qaverdict.StatusPass)
	if code, text := gate(inBurst); code != pushgate.ExitOK {
		t.Fatalf("passing verdict, same burst: exit %d: %s", code, text)
	}
	after := commitAt(t, repo, base.Add(time.Hour), "made after qa-all ended")
	if code, text := gate(after); code != pushgate.ExitRefused || !strings.Contains(text, after[:12]) {
		t.Fatalf("commit after the run: exit %d: %s", code, text)
	}
	git(t, repo, nil, "checkout", "-q", "-b", "side", published)
	side := commitAt(t, repo, base.Add(3*time.Minute), "a branch the run never judged")
	if code, text := gate(side); code != pushgate.ExitRefused || !strings.Contains(text, "not in what is pushed") {
		t.Fatalf("unrelated branch: exit %d: %s", code, text)
	}
	if code, text := gate(published); code != pushgate.ExitOK {
		t.Fatalf("nothing new: exit %d: %s", code, text)
	}
}
