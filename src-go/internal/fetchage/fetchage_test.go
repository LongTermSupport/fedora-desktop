package fetchage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// now is the fixed clock every case reads; no test reads the wall clock.
var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

const remoteConfig = "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = git@example.com:o/r.git\n"

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fetchRecord is a line of the FETCH_HEAD that a fetch which reached the remote writes.
const fetchRecord = "0123456789abcdef0123456789abcdef01234567\t\tbranch 'main' of example.com:o/r\n"

// stamp writes content to path, dated at.
func stamp(t *testing.T, path, content string, at time.Time) {
	t.Helper()
	write(t, path, content)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// touch records a fetch that reached the remote, at.
func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	stamp(t, path, fetchRecord, at)
}

// attempt leaves what git leaves of a fetch that failed or is still running: git empties
// FETCH_HEAD before it contacts the remote.
func attempt(t *testing.T, path string, at time.Time) {
	t.Helper()
	stamp(t, path, "", at)
}

// repo makes a checkout at root/repo whose .git holds config, and returns the checkout.
func repo(t *testing.T, config string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(dir, ".git", "config"), config)
	return dir
}

func mark(t *testing.T, dir string) string {
	t.Helper()
	got, err := Mark(dir, now)
	if err != nil {
		t.Fatalf("Mark(%s): %v", dir, err)
	}
	return got
}

// A1: outside any checkout there is nothing to say.
func TestNoRepositoryIsNoMark(t *testing.T) {
	if got := mark(t, t.TempDir()); got != "" {
		t.Fatalf("got %q, want no mark", got)
	}
}

// A2: a repository with no remote has no tracking ref to go stale.
func TestNoRemoteIsNoMark(t *testing.T) {
	dir := repo(t, "[core]\n\tbare = false\n")
	if got := mark(t, dir); got != "" {
		t.Fatalf("got %q, want no mark", got)
	}
}

// A3: a fetch within the threshold is fresh.
func TestRecentFetchIsNoMark(t *testing.T) {
	dir := repo(t, remoteConfig)
	touch(t, filepath.Join(dir, ".git", "FETCH_HEAD"), now.Add(-Threshold+time.Minute))
	if got := mark(t, dir); got != "" {
		t.Fatalf("got %q, want no mark", got)
	}
}

// A4: an old fetch is marked with its age, from a subdirectory too.
func TestOldFetchIsMarkedWithItsAge(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want string
	}{
		{Threshold, " [fetched 1h ago]"},
		{5*time.Hour + 59*time.Minute, " [fetched 5h ago]"},
		{47 * time.Hour, " [fetched 47h ago]"},
		{48 * time.Hour, " [fetched 2d ago]"},
		{14*24*time.Hour + 3*time.Hour, " [fetched 14d ago]"},
	}
	for _, c := range cases {
		dir := repo(t, remoteConfig)
		touch(t, filepath.Join(dir, ".git", "FETCH_HEAD"), now.Add(-c.age))
		sub := filepath.Join(dir, "a", "b")
		if err := os.MkdirAll(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		if got := mark(t, sub); got != c.want {
			t.Errorf("age %v: got %q, want %q", c.age, got, c.want)
		}
	}
}

// A5: a remote and no fetch recorded at all.
func TestNoFetchRecordedIsMarked(t *testing.T) {
	dir := repo(t, remoteConfig)
	if got := mark(t, dir); got != " [no fetch recorded]" {
		t.Fatalf("got %q", got)
	}
}

// A6: a FETCH_HEAD dated in the future (a clock step) counts as fresh.
func TestFutureFetchIsNoMark(t *testing.T) {
	dir := repo(t, remoteConfig)
	touch(t, filepath.Join(dir, ".git", "FETCH_HEAD"), now.Add(time.Hour))
	if got := mark(t, dir); got != "" {
		t.Fatalf("got %q, want no mark", got)
	}
}

// linked makes a main checkout and a linked worktree of it, as git worktree add lays them out
// (relative commondir, absolute gitdir), and returns the main git dir and the worktree.
func linked(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	common := filepath.Join(root, "main", ".git")
	write(t, filepath.Join(common, "config"), remoteConfig)
	wtGit := filepath.Join(common, "worktrees", "wt")
	write(t, filepath.Join(wtGit, "commondir"), "../..\n")
	wt := filepath.Join(root, "wt")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+wtGit+"\n")
	return common, wt
}

// A7: git keeps FETCH_HEAD per worktree while the tracking refs are shared, so the newest
// FETCH_HEAD of any worktree of the repository is the last fetch.
func TestLinkedWorktreeReadsEveryWorktreesFetch(t *testing.T) {
	common, wt := linked(t)
	touch(t, filepath.Join(common, "FETCH_HEAD"), now.Add(-30*24*time.Hour))
	if got := mark(t, wt); got != " [fetched 30d ago]" {
		t.Fatalf("main fetch only: got %q", got)
	}
	touch(t, filepath.Join(common, "worktrees", "other", "FETCH_HEAD"), now.Add(-3*time.Hour))
	if got := mark(t, wt); got != " [fetched 3h ago]" {
		t.Fatalf("another worktree's fetch: got %q", got)
	}
	touch(t, filepath.Join(common, "worktrees", "wt", "FETCH_HEAD"), now.Add(-time.Minute))
	if got := mark(t, wt); got != "" {
		t.Fatalf("own fresh fetch: got %q", got)
	}
	if got := mark(t, filepath.Dir(common)); got != "" {
		t.Fatalf("main checkout sees the worktree's fetch: got %q", got)
	}
}

// A8: a relative gitdir (a submodule's) is read against the checkout, and a git dir with no
// commondir is its own common dir.
func TestRelativeGitdirWithoutCommondir(t *testing.T) {
	root := t.TempDir()
	modGit := filepath.Join(root, ".git", "modules", "sub")
	write(t, filepath.Join(modGit, "config"), remoteConfig)
	touch(t, filepath.Join(modGit, "FETCH_HEAD"), now.Add(-72*time.Hour))
	sub := filepath.Join(root, "sub")
	write(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/sub\n")
	if got := mark(t, sub); got != " [fetched 3d ago]" {
		t.Fatalf("got %q", got)
	}
}

// A9: a .git file that is not a gitdir line, or a git dir with no config, is an error, never
// a quiet "fresh".
func TestBrokenRepositoryIsAnError(t *testing.T) {
	bad := t.TempDir()
	write(t, filepath.Join(bad, ".git"), "not a gitdir line\n")
	if _, err := Mark(bad, now); err == nil {
		t.Fatal("malformed .git file: no error")
	}
	noConfig := t.TempDir()
	if err := os.MkdirAll(filepath.Join(noConfig, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Mark(noConfig, now); err == nil {
		t.Fatal("git dir without config: no error")
	}
	gone := t.TempDir()
	write(t, filepath.Join(gone, ".git"), "gitdir: "+filepath.Join(gone, "missing")+"\n")
	if _, err := Mark(gone, now); err == nil {
		t.Fatal("gitdir pointing nowhere: no error")
	}
}

// A14: git empties FETCH_HEAD before it contacts the remote, so a fetch that failed (no key
// in the agent, no network) leaves it empty and newly dated, as does one still running. That
// is never a fetch: it is marked however young it is.
func TestEmptyFetchHeadIsAFailedOrRunningFetch(t *testing.T) {
	for _, at := range []time.Time{now.Add(-time.Second), now.Add(-14 * 24 * time.Hour)} {
		dir := repo(t, remoteConfig)
		attempt(t, filepath.Join(dir, ".git", "FETCH_HEAD"), at)
		if got := mark(t, dir); got != FailedMark {
			t.Errorf("empty FETCH_HEAD dated %v: got %q, want %q", at, got, FailedMark)
		}
	}
}

// A15: across worktrees the last fetch is the newest FETCH_HEAD with a record in it. A fresh
// one means the shared tracking refs are fresh, whatever failed since; a failed attempt newer
// than a stale fetch is marked as failed; a stale fetch newer than a failed attempt by its age.
func TestLinkedWorktreesWeighFailedAttemptsAgainstTheLastFetch(t *testing.T) {
	common, wt := linked(t)
	touch(t, filepath.Join(common, "FETCH_HEAD"), now.Add(-10*time.Minute))
	attempt(t, filepath.Join(common, "worktrees", "wt", "FETCH_HEAD"), now.Add(-time.Minute))
	if got := mark(t, wt); got != "" {
		t.Fatalf("fresh fetch elsewhere, failed attempt here: got %q", got)
	}
	touch(t, filepath.Join(common, "FETCH_HEAD"), now.Add(-5*24*time.Hour))
	if got := mark(t, wt); got != FailedMark {
		t.Fatalf("stale fetch, newer failed attempt: got %q", got)
	}
	attempt(t, filepath.Join(common, "worktrees", "wt", "FETCH_HEAD"), now.Add(-6*24*time.Hour))
	if got := mark(t, wt); got != " [fetched 5d ago]" {
		t.Fatalf("stale fetch newer than the failed attempt: got %q", got)
	}
}

func app(dir string) (App, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return App{Dir: dir, Now: now, Stdout: &out, Stderr: &errOut}, &out, &errOut
}

// A10: the mark is the whole of stdout; nothing else is ever written there.
func TestRunPrintsOnlyTheMark(t *testing.T) {
	dir := repo(t, remoteConfig)
	a, out, errOut := app(dir)
	if code := a.Run(nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out.String() != " [no fetch recorded]\n" || errOut.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
	a, out, errOut = app(t.TempDir())
	if code := a.Run(nil); code != term.ExitOK || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("no repo: exit %d stdout %q stderr %q", code, out, errOut)
	}
}

// A11: an error goes to stderr with exit 1 and leaves stdout empty.
func TestRunReportsAnErrorOnStderr(t *testing.T) {
	bad := t.TempDir()
	write(t, filepath.Join(bad, ".git"), "junk\n")
	a, out, errOut := app(bad)
	if code := a.Run(nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 || !bytes.HasPrefix(errOut.Bytes(), []byte("git-fetch-age: ")) {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
}

// A12: the command takes no arguments: any is a usage error on stderr.
func TestRunRefusesArguments(t *testing.T) {
	a, out, errOut := app(t.TempDir())
	if code := a.Run([]string{"--fetch"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 || errOut.Len() == 0 {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// A13: a mark that could not be written is a failure, not a success.
func TestRunFailsWhenStdoutFails(t *testing.T) {
	dir := repo(t, remoteConfig)
	var errOut bytes.Buffer
	a := App{Dir: dir, Now: now, Stdout: failingWriter{}, Stderr: &errOut}
	if code := a.Run(nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}
