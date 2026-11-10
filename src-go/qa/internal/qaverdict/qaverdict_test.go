package qaverdict

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func at(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

func TestWriteThenReadKeepsTheRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	want := Record{Status: StatusPass, Commit: sha, Started: at(100), Finished: at(200)}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("read %+v, wrote %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Fatalf("mode %v, want %v", info.Mode().Perm(), os.FileMode(fileMode))
	}
}

func TestWriteRefusesARecordThatSaysNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	for _, rec := range []Record{
		{Status: "maybe", Commit: sha, Started: at(1), Finished: at(2)},
		{Status: StatusPass, Commit: "HEAD", Started: at(1), Finished: at(2)},
		{Status: StatusPass, Commit: sha, Finished: at(2)},
		{Status: StatusPass, Commit: sha, Started: at(3), Finished: at(2)},
	} {
		if err := Write(path, rec); err == nil {
			t.Fatalf("wrote %+v", rec)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a refused record left a file: %v", err)
		}
	}
}

func TestReadAnswersNoneForAMissingFile(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), FileName))
	if !errors.Is(err, ErrNone) {
		t.Fatalf("err %v, want ErrNone", err)
	}
}

func TestReadRefusesADamagedRecord(t *testing.T) {
	for _, body := range []string{
		"",
		"{",
		`{"status":"pass"}`,
		`{"status":"pass","commit":"` + sha + `","started":"1970-01-01T00:01:40Z","finished":"1970-01-01T00:03:20Z","extra":1}`,
		`{"status":"PASS","commit":"` + sha + `","started":"1970-01-01T00:01:40Z","finished":"1970-01-01T00:03:20Z"}`,
	} {
		path := filepath.Join(t.TempDir(), FileName)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Read(path)
		if err == nil || errors.Is(err, ErrNone) {
			t.Fatalf("read %q: err %v, want a damaged-record error", body, err)
		}
	}
}

func TestRemoveClearsAndToleratesNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, Record{Status: StatusFail, Commit: sha, Started: at(1), Finished: at(1)}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("still there: %v", err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: append([]string{"git", "-C", dir}, args...),
		Env: []string{
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		},
	})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %d\n%s", args, err, res.Code, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

func TestLocateNamesHeadAndTheCommonDirectoryOfAWorktree(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "one")
	head := gitIn(t, repo, "rev-parse", "HEAD")
	linked := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "side", linked)
	gitIn(t, linked, "commit", "-q", "--allow-empty", "-m", "two")
	sideHead := gitIn(t, linked, "rev-parse", "HEAD")
	common := gitIn(t, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")

	got, err := Locate(context.Background(), proc.OS{}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != head || got.Path != filepath.Join(common, FileName) {
		t.Fatalf("main checkout: %+v, want commit %s path under %s", got, head, common)
	}
	got, err = Locate(context.Background(), proc.OS{}, linked)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != sideHead || got.Path != filepath.Join(common, FileName) {
		t.Fatalf("linked worktree: %+v, want commit %s, the shared path", got, sideHead)
	}
}

func TestLocateRefusesARepositoryWithoutACommit(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	if _, err := Locate(context.Background(), proc.OS{}, repo); err == nil {
		t.Fatal("located a verdict target in a repository with no commit")
	}
}

func TestLocateRefusesADirectoryOutsideGit(t *testing.T) {
	if _, err := Locate(context.Background(), proc.OS{}, t.TempDir()); err == nil {
		t.Fatal("located a verdict target outside git")
	}
}
