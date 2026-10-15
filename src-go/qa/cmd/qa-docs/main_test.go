package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/docs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(docs.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(docs.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// D2: a closed stdout cannot deliver the verdict, so the command refuses to run.
func TestClosedStdoutIsExitTwo(t *testing.T) {
	if os.Getenv(closedStdoutChild) != "" {
		os.Exit(run([]string{"--help"}))
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestClosedStdoutIsExitTwo$' >&-`, self},
		Env:  []string{closedStdoutChild + "=1"},
	})
	if runErr != nil || res.Code != int(docs.ExitCannotRun) || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("closed stdout: %v exit %d\nstderr:\n%s", runErr, res.Code, res.Stderr)
	}
}

// closedStdoutChild names the environment variable that turns this test binary into the command.
const closedStdoutChild = "QA_DOCS_CLOSED_STDOUT_CHILD"

// outcome is what one run over a real work tree produced.
type outcome struct {
	code   docs.ExitCode
	stdout string
	stderr string
	report string
}

// base is a tree every check passes on.
var base = map[string]string{
	"scripts/qa-trees.tsv":         "shell\t.git\nshell\tuntracked\nshell\t.claude/hooks-daemon\nshell\t.claude/worktrees\npython\t.venv\n",
	"scripts/qa-gates.tsv":         "gate\tg1\tscripts/g1.bash\ttext\tcount\n",
	"scripts/g1.bash":              "#!/usr/bin/bash\n",
	"CLAUDE/QA.md":                 "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | x |\n",
	"CLAUDE.md":                    "# Index\n\nCLAUDE/QA.md\n",
	"docs/playbooks.md":            "# P\n\nplay-a.yml\n",
	"docs/architecture.md":         "# A\n\nplay-a.yml\n",
	"playbooks/playbook-main.yml":  "---\n- import_playbook: imports/play-a.yml\n",
	"playbooks/imports/play-a.yml": "- hosts: all\n",
}

// git runs the real git in the directory and fails the test unless it succeeds.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: dir})
	if err != nil || res.Code != 0 {
		t.Fatalf("git %v: %v %+v", args, err, res)
	}
}

// writeTree writes the files under root; an empty text writes nothing.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		if text == "" {
			continue
		}
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// repo makes a real work tree of base overlaid by files, everything staged.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	all := maps.Clone(base)
	maps.Copy(all, files)
	writeTree(t, root, all)
	git(t, root, "add", "-A")
	return root
}

// runIn runs the gate with the real git and file system over a root.
func runIn(t *testing.T, root string) outcome {
	t.Helper()
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := docs.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, ReadFile: os.ReadFile, Stat: os.Stat, Lstat: os.Lstat, WriteFile: cmdmain.WriteReport,
	}
	code := docs.Main([]string{"--root", root}, env)
	got := outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
	if data, readErr := os.ReadFile(filepath.Clean(reportPath)); readErr == nil {
		got.report = string(data)
	}
	return got
}

// D2, D22: the real git and file system judge a clean tree.
func TestRealCleanTreePasses(t *testing.T) {
	got := runIn(t, repo(t, nil))
	want := "✓ docs: 4 files (4 tracked) OK (links, anchors, playbook catalogue, topic index, gate inventory) — VENDORED: 0 verified, 0 unverifiable (repo absent), 0 broken\n"
	if got.code != docs.ExitOK || got.stdout != want || got.stderr != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// D12: a link to a file nobody added fails, though it is on disk; one to a tracked file does not.
func TestRealLinkToAnUntrackedFileFailsAndToATrackedOneDoesNot(t *testing.T) {
	root := repo(t, map[string]string{
		"docs/index.md": "# I\n\n[a](./kept.md) [b](./loose.md)\n",
		"docs/kept.md":  "# F\n",
		"docs/loose.md": "# F\n",
	})
	// loose.md is now on disk and not in the index, but not ignored: published, never tracked.
	git(t, root, "rm", "-q", "--cached", "docs/loose.md")
	got := runIn(t, root)
	if got.code != docs.ExitFailed || !strings.Contains(got.stdout, "docs/index.md:3  ./loose.md  — target is not tracked by this repository") ||
		strings.Contains(got.stdout, "kept.md  —") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "across 7 files (6 tracked)") {
		t.Fatalf("the denominator does not show the untracked document:\n%s", got.stdout)
	}
}

// D4: an ignored document is not scanned, and CLAUDE/Plan never is.
func TestRealIgnoredAndPlanDocumentsAreNotScanned(t *testing.T) {
	root := repo(t, map[string]string{
		".gitignore":                    "docs/ignored.md\n",
		"docs/ignored.md":               "[x](./nope.md)\n",
		"CLAUDE/Plan/00001-x/PLAN.md":   "[x](./nope.md)\n",
		"CLAUDE/Plan/Completed/a/B.md":  "[x](./nope.md)\n",
		"untracked/notes/CLAUDE.md":     "[x](./nope.md)\n",
		".claude/worktrees/w/CLAUDE.md": "[x](./nope.md)\n",
	})
	got := runIn(t, root)
	if got.code != docs.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// D5: a tree with no in-scope document is exit 2.
func TestRealEmptyScopeIsExitTwo(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	writeTree(t, root, map[string]string{"scripts/qa-trees.tsv": base["scripts/qa-trees.tsv"], "other.md": "# x\n"})
	got := runIn(t, root)
	if got.code != docs.ExitCannotRun || !strings.Contains(got.stderr, "found 0 in-scope markdown files") || got.stdout != "" || got.report != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// D25: a tree git cannot answer for is exit 2, never a verdict that every link is untracked.
func TestRealTreeGitCannotAnswerForIsExitTwo(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"scripts/qa-trees.tsv": base["scripts/qa-trees.tsv"], "docs/a.md": "[x](./missing.md)\n"})
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	got := runIn(t, root)
	if got.code != docs.ExitCannotRun || !strings.HasPrefix(got.stderr, "ERROR: ") || strings.Contains(got.stderr, root) || got.stdout != "" || got.report != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// D24: a root reached through a symlink prints no checkout path anywhere.
func TestRealSymlinkedRootNeverLeaksThePath(t *testing.T) {
	root := repo(t, map[string]string{"docs/bad.md": "[x](./nope.md)\n"})
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	got := runIn(t, link)
	if got.code != docs.ExitFailed || !strings.Contains(got.stdout, "docs/bad.md:1  ./nope.md") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, leak := range []string{root, link} {
		for name, text := range map[string]string{"stdout": got.stdout, "stderr": got.stderr, "report": got.report} {
			if strings.Contains(text, leak) {
				t.Fatalf("%s names the checkout %s:\n%s", name, leak, text)
			}
		}
	}
}

// B15: an untracked nested repository is exit 2 from the shared discovery, which cannot look into it.
func TestRealUntrackedNestedRepositoryStopsTheRun(t *testing.T) {
	root := repo(t, nil)
	nested := filepath.Join(root, "scratch", "clone")
	git(t, root, "init", "-q", nested)
	writeTree(t, nested, map[string]string{"README.md": "# c\n"})
	got := runIn(t, root)
	if got.code != docs.ExitCannotRun || !strings.Contains(got.stderr, "nested repository") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}
