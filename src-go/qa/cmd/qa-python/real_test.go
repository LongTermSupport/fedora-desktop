package main

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/python"
)

// Y22: a ruff config nested under the root cannot replace the repository's ruleset, whether it
// is a pyproject.toml or a ruff.toml; the file is still listed by --show-files, so only the
// pinned --config keeps the verdict honest.
func TestRealNestedRuffConfigCannotSwapTheRuleset(t *testing.T) {
	for name, nested := range map[string]map[string]string{
		"pyproject.toml": {"sub/pyproject.toml": "[tool.ruff.lint]\nselect = []\n"},
		"ruff.toml":      {"sub/ruff.toml": "[lint]\nselect = []\n"},
		".ruff.toml":     {"sub/.ruff.toml": "[lint]\nselect = []\n"},
	} {
		files := map[string]string{"sub/bad.py": "import os\nprint(undefined)\n"}
		maps.Copy(files, nested)
		got := realRun(t, files)
		if got.code != python.ExitFailed || !strings.Contains(got.report, `"filename": "sub/bad.py"`) ||
			!strings.Contains(got.report, "F401") || !strings.Contains(got.report, "F821") {
			t.Fatalf("%s: exit %d\nstdout:\n%s\nstderr:\n%s\nreport:\n%s", name, got.code, got.stdout, got.stderr, got.report)
		}
	}
}

// Y23: a noqa directive, line or file wide, cannot hide a finding.
func TestRealNoqaDirectivesDoNotHideFindings(t *testing.T) {
	got := realRun(t, map[string]string{
		"file_wide.py": "# ruff: noqa\nimport os\n",
		"inline.py":    "import sys  # noqa: F401\n",
	})
	if got.code != python.ExitFailed || !strings.Contains(got.report, `"filename": "file_wide.py"`) || !strings.Contains(got.report, `"filename": "inline.py"`) {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s\nreport:\n%s", got.code, got.stdout, got.stderr, got.report)
	}
}

// Y1: the real tools with a relative root and with a trailing slash.
func TestRealToolsAcceptARelativeAndATrailingSlashRoot(t *testing.T) {
	for name, args := range map[string]func(string) []string{
		"relative":       func(string) []string { return []string{"--root", "."} },
		"trailing slash": func(root string) []string { return []string{"--root", root + "/"} },
	} {
		got := realRunWith(t, map[string]string{"good.py": "print(1)\n"}, args)
		if got.code != python.ExitOK {
			t.Fatalf("%s: exit %d\nstdout:\n%s\nstderr:\n%s", name, got.code, got.stdout, got.stderr)
		}
	}
}

// closedStdoutChild names the environment variable that turns this test binary into the command.
const closedStdoutChild = "QA_PYTHON_CLOSED_STDOUT_CHILD"

// Y3: with stdout closed (>&-) the run is exit 2, it does not report a verdict nobody can read.
// The test re-runs its own binary as the command, with fd 1 closed by the shell.
func TestAClosedStdoutIsExitTwo(t *testing.T) {
	if args := os.Getenv(closedStdoutChild); args != "" {
		os.Exit(run(strings.Fields(args)))
	}
	root := t.TempDir()
	if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root}); err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	ver, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"ruff", "--version"}})
	if err != nil || ver.Code != 0 {
		t.Fatalf("ruff is needed for this test: %v %+v", err, ver)
	}
	for rel, text := range map[string]string{
		".qa-versions": "RUFF=" + strings.Fields(ver.Stdout)[1] + "\n", "scripts/qa-trees.tsv": "shell\t.git\npython\t.venv\n",
		"ruff.toml": "[lint]\nselect = [\"F\"]\n", "good.py": "print(1)\n",
	} {
		path := filepath.Join(root, rel)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wrErr := os.WriteFile(path, []byte(text), 0o600); wrErr != nil {
			t.Fatal(wrErr)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestAClosedStdoutIsExitTwo$' >&-`, self},
		Env:  []string{closedStdoutChild + "=--root " + root, "QA_JSON_OUT=" + filepath.Join(t.TempDir(), "r.json")},
	})
	if runErr != nil || res.Code != int(python.ExitCannotRun) || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("closed stdout: %v exit %d\nstderr:\n%s", runErr, res.Code, res.Stderr)
	}
}
