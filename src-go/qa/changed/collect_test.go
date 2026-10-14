package changed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// gitIn runs git in dir for a test fixture, failing the test on any error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: argv, Dir: dir, Env: []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %+v", args, err, res)
	}
	return strings.TrimSpace(res.Stdout)
}

// scratchRepo is a committed repository with one kept file, one to delete and an ignored name.
func scratchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeFile(t, dir, ".gitignore", "ignored.bash\n")
	writeFile(t, dir, "kept.bash", "#!/usr/bin/bash\n:\n")
	writeFile(t, dir, "gone.bash", "#!/usr/bin/bash\n:\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func sessionAt(t *testing.T, root string) *session {
	t.Helper()
	return &session{env: Env{Runner: proc.OS{}, Stderr: &strings.Builder{}}, root: root, scripts: filepath.Join(root, "scripts")}
}

func TestCollectGitFindsModifiedUntrackedAndDeletedButNotIgnored(t *testing.T) {
	dir := scratchRepo(t)
	writeFile(t, dir, "kept.bash", "#!/usr/bin/bash\n: changed\n")
	if err := os.Remove(filepath.Join(dir, "gone.bash")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "new.py", "x = 1\n")
	writeFile(t, dir, "ignored.bash", ":\n")
	got, err := sessionAt(t, dir).collectGit(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.existing, []string{"kept.bash", "new.py"}) || !reflect.DeepEqual(got.deleted, []string{"gone.bash"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestCollectGitBaseCoversCommittedChanges(t *testing.T) {
	dir := scratchRepo(t)
	base := gitIn(t, dir, "rev-parse", "HEAD")
	writeFile(t, dir, "kept.bash", ":\n: more\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "second")
	s := sessionAt(t, dir)
	clean, err := s.collectGit(context.Background(), "HEAD")
	if err != nil || len(clean.existing)+len(clean.deleted) != 0 {
		t.Fatalf("a clean tree: %+v %v", clean, err)
	}
	got, err := s.collectGit(context.Background(), base)
	if err != nil || !reflect.DeepEqual(got.existing, []string{"kept.bash"}) {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestCollectGitFailureIsAnErrorNotAnEmptyList(t *testing.T) {
	dir := scratchRepo(t)
	_, err := sessionAt(t, dir).collectGit(context.Background(), "nosuchref")
	if err == nil || !strings.Contains(err.Error(), "cannot list changed files vs nosuchref") {
		t.Fatalf("a bad ref must fail loudly: %v", err)
	}
	_, err = sessionAt(t, t.TempDir()).collectGit(context.Background(), "HEAD")
	if err == nil {
		t.Fatal("a directory with no repository must fail loudly")
	}
}

func TestCollectGitNotesWhatIsNotARegularFile(t *testing.T) {
	dir := scratchRepo(t)
	if err := os.Symlink("nowhere", filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "emptydir"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "real.md", "# x\n")
	got, err := sessionAt(t, dir).collectGit(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.existing, []string{"real.md"}) {
		t.Fatalf("got %+v", got)
	}
	if len(got.notes) != 1 || !strings.Contains(got.notes[0], "dangling") {
		t.Fatalf("a dropped path is named, not silent: %v", got.notes)
	}
}

func TestCollectGitReportsEachGitFailure(t *testing.T) {
	for _, failing := range []string{"git diff --name-only -z --diff-filter=d", "git diff --name-only -z --diff-filter=D", "git ls-files"} {
		runner := &scriptedRunner{replies: map[string]proc.Result{failing: {Stderr: "fatal: boom", Code: 128}}}
		s := &session{env: Env{Runner: runner}, root: t.TempDir()}
		if _, err := s.collectGit(context.Background(), "HEAD"); err == nil || !strings.Contains(err.Error(), "fatal: boom") {
			t.Errorf("%s: %v", failing, err)
		}
	}
	s := &session{env: Env{Runner: failingRunner{}}, root: t.TempDir()}
	if _, err := s.collectGit(context.Background(), "HEAD"); err == nil {
		t.Fatal("a git that cannot start must be an error")
	}
}

func TestCollectExplicitNormalisesAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "scripts/a.bash", ":\n")
	writeFile(t, root, "b.md", "x\n")
	s := &session{root: root, env: Env{Stderr: &strings.Builder{}}}
	got, err := s.collectExplicit([]string{"./scripts/a.bash", "scripts/a.bash", filepath.Join(root, "b.md"), "scripts/../b.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.existing, []string{"scripts/a.bash", "b.md"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestCollectExplicitRefusesWhatIsOutsideOrNotAFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "d/x", "x\n")
	s := &session{root: root, env: Env{Stderr: &strings.Builder{}}}
	for name, arg := range map[string]string{
		"outside by ..":  "../etc/passwd",
		"outside by abs": "/etc/passwd",
		"the root":       ".",
		"a directory":    "d",
	} {
		if _, err := s.collectExplicit([]string{arg}); err == nil {
			t.Errorf("%s: %q was accepted", name, arg)
		}
	}
}

func TestCollectExplicitDropsAMissingPathWithAWord(t *testing.T) {
	var errBuf strings.Builder
	s := &session{root: t.TempDir(), env: Env{Stderr: &errBuf}}
	got, err := s.collectExplicit([]string{"no/such/file.bash"})
	if err != nil || len(got.existing) != 0 || len(got.deleted) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
	if !strings.Contains(errBuf.String(), "no/such/file.bash") {
		t.Fatalf("the drop must be said: %q", errBuf.String())
	}
}
