package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/branchhygiene"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/pushgate"
)

// realGit runs git for real, answers the local docs check with docsCode, and fails the test if
// gh is asked anything; it records the docs check's argv.
type realGit struct {
	t         *testing.T
	docsCode  int
	docsArgv  []string
	askedGH   bool
	ranGitEnv []string
}

func (r *realGit) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	switch {
	case c.Argv[0] == "git":
		c.Env = append(c.Env, r.ranGitEnv...)
		return proc.OS{}.Run(ctx, c)
	case c.Argv[0] == "gh":
		r.askedGH = true
		return proc.Result{Stdout: `{"totalCount":0,"runs":[]}`}, nil
	case strings.HasSuffix(c.Argv[0], "/scripts/qa-changed.bash"):
		r.docsArgv = c.Argv
		return proc.Result{Code: r.docsCode}, nil
	default:
		r.t.Errorf("unexpected command %v", c.Argv)
		return proc.Result{Code: 1}, nil
	}
}

// published is a repository whose F44 holds the branch policy and the real qa.yml, then a
// commit changing these files on top; it answers the ref line pushing that commit to F44.
func published(t *testing.T, change map[string]string) (string, string) {
	t.Helper()
	data, err := os.ReadFile("../../../../" + pushgate.QAWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := os.ReadFile("../../../../" + branchhygiene.PolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "F44")
	write(t, repo, map[string]string{pushgate.QAWorkflow: string(data), branchhygiene.PolicyFile: string(pol), "docs/old.md": "old\n", "main.go": "package main\n"})
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "published")
	base := git(t, repo, "rev-parse", "HEAD")
	write(t, repo, change)
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	head := git(t, repo, "rev-parse", "HEAD")
	return repo, "refs/heads/F44 " + head + " refs/heads/F44 " + base + "\n"
}

func write(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func gate(t *testing.T, repo, line string, runner *realGit) (pushgate.ExitCode, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := pushgate.Main(context.Background(), []string{"origin", "git@github.com:o/r.git"}, pushgate.Env{
		Stdin: strings.NewReader(line), Stdout: &out, Stderr: &errOut, Runner: runner, Cwd: repo,
		Getenv: func(string) string { return "" }, ReadFile: os.ReadFile,
	})
	return code, out.String() + errOut.String()
}

// A docs-only commit over F44, read by real git against the real qa.yml: the local docs check
// runs on the range, and CI is not asked.
func TestADocsOnlyCommitIsJudgedByTheLocalDocsCheck(t *testing.T) {
	repo, line := published(t, map[string]string{"docs/new.md": "new\n", "README.md": "readme\n"})
	runner := &realGit{t: t, ranGitEnv: gitEnv}
	code, text := gate(t, repo, line, runner)
	if code != pushgate.ExitOK || runner.askedGH {
		t.Fatalf("exit %d, asked gh %t: %s", code, runner.askedGH, text)
	}
	base := strings.Fields(line)[3]
	if want := []string{filepath.Join(repo, "scripts/qa-changed.bash"), "--base", base}; strings.Join(runner.docsArgv, " ") != strings.Join(want, " ") {
		t.Fatalf("docs check ran as %v, want %v", runner.docsArgv, want)
	}
	if !strings.Contains(text, "2 files") {
		t.Fatalf("the verdict does not count the range: %s", text)
	}
}

// A deletion of documentation is docs-only; a deletion or a rename touching code is not.
func TestDeletionsAndRenamesCountEachPath(t *testing.T) {
	repo, line := published(t, nil)
	git(t, repo, "rm", "-q", "docs/old.md")
	git(t, repo, "commit", "-q", "-m", "drop a doc")
	head := git(t, repo, "rev-parse", "HEAD")
	base := strings.Fields(line)[3]
	runner := &realGit{t: t, ranGitEnv: gitEnv}
	if code, text := gate(t, repo, "refs/heads/F44 "+head+" refs/heads/F44 "+base+"\n", runner); code != pushgate.ExitOK || runner.askedGH {
		t.Fatalf("a doc deleted: exit %d: %s", code, text)
	}
	git(t, repo, "mv", "main.go", "main.md")
	git(t, repo, "commit", "-q", "-m", "code renamed to Markdown")
	head = git(t, repo, "rev-parse", "HEAD")
	runner = &realGit{t: t, ranGitEnv: gitEnv}
	if code, text := gate(t, repo, "refs/heads/F44 "+head+" refs/heads/F44 "+base+"\n", runner); code != pushgate.ExitRefused || !runner.askedGH || runner.docsArgv != nil {
		t.Fatalf("code renamed to Markdown: exit %d, asked gh %t: %s", code, runner.askedGH, text)
	}
}

// A dirty checkout cannot stand in for the commit: CI is asked.
func TestADirtyCheckoutAsksCI(t *testing.T) {
	repo, line := published(t, map[string]string{"docs/new.md": "new\n"})
	write(t, repo, map[string]string{"notes.md": "untracked\n"})
	runner := &realGit{t: t, ranGitEnv: gitEnv}
	code, text := gate(t, repo, line, runner)
	if code != pushgate.ExitRefused || !runner.askedGH || runner.docsArgv != nil {
		t.Fatalf("exit %d, asked gh %t: %s", code, runner.askedGH, text)
	}
	if !strings.Contains(text, "untracked") {
		t.Fatalf("does not say why: %s", text)
	}
}
