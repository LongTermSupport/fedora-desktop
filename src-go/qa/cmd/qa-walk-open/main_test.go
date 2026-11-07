package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/sttrules"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	var stdout bytes.Buffer
	env := sttrules.Env{Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir(), Runner: proc.OS{}}
	if code := sttrules.WalkOpen.Main([]string{"--help"}, env); code != sttrules.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "usage: qa-walk-open ") {
		t.Fatalf("usage:\n%s", stdout.String())
	}
	if code := run([]string{"--help"}); code != int(sttrules.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(sttrules.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// projectFile reads a file of the real repository by its path from this package.
func projectFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// outcome is what one run over a real work tree produced.
type outcome struct {
	code           sttrules.ExitCode
	stdout, stderr string
}

// realRun runs the gate with the real git and semgrep over a work tree holding the project's own
// rule file and fixture plus files, so the rule file semgrep reads is the one the gate ships.
func realRun(t *testing.T, files map[string]string, args ...string) outcome {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"scripts/qa-trees.tsv":   "shell\t.semgrep\npython\t.venv\n",
		".semgrep/walk-open.yml": projectFile(t, ".semgrep/walk-open.yml"),
		".semgrep/walk-open.py":  projectFile(t, ".semgrep/walk-open.py"),
		"helpers/clean.py":       "print('hi')\n",
		"extensions/panel.js":    "export const a = 1;\n",
	}
	maps.Copy(all, files)
	for rel, text := range all {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"git", "init", "-q"}, {"git", "add", "-A"}} {
		if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: argv, Dir: root}); err != nil || res.Code != 0 {
			t.Fatalf("%v: %v %+v", argv, err, res)
		}
	}
	var stdout, stderr bytes.Buffer
	env := sttrules.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	code := sttrules.WalkOpen.Main(append([]string{"--root", root}, args...), env)
	if strings.Contains(stdout.String()+stderr.String(), root) {
		t.Fatalf("the checkout path is in the output:\n%s%s", stdout.String(), stderr.String())
	}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

const walkedAndRead = `import os


def texts(top):
    for dirpath, _dirs, files in os.walk(top):
        for name in files:
            with open(os.path.join(dirpath, name), encoding="utf-8") as handle:
                yield handle.read()
`

// The real semgrep, with the project's rule file, passes a clean tree and reads only Python.
func TestRealSemgrepPassesACleanTree(t *testing.T) {
	got := realRun(t, nil)
	if got.code != sttrules.ExitOK || got.stdout != "passed: 1 failed: 0\n" {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// A walked entry opened without a regular-file check is reported under the gate's own heading.
func TestRealSemgrepReportsAWalkedEntryOpenedUnchecked(t *testing.T) {
	got := realRun(t, map[string]string{"helpers/walker.py": walkedAndRead})
	if got.code != sttrules.ExitFailed {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"helpers/walker.py:7: walk-opens-non-regular-file\n",
		"walk-open rule: 1 finding(s) in 1 file(s).",
		"passed: 1 failed: 1\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

// The daemon-owned supervisor is left out of the default scope, so its findings are upstream's.
func TestTheDaemonOwnedSupervisorIsNotJudged(t *testing.T) {
	got := realRun(t, map[string]string{".claude/ccy/claude-supervise.py": walkedAndRead})
	if got.code != sttrules.ExitOK || got.stdout != "passed: 1 failed: 0\n" {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// The rule edited into one its fixture disproves (a directory test taken for a regular-file check)
// fails its fixture, rather than reporting a clean tree.
func TestRealSemgrepFailsARuleItsFixtureDisproves(t *testing.T) {
	rules := strings.ReplaceAll(projectFile(t, ".semgrep/walk-open.yml"), "os.path.isfile($X)", "os.path.isdir($X)")
	got := realRun(t, map[string]string{".semgrep/walk-open.yml": rules})
	if got.code != sttrules.ExitFailed || !strings.Contains(got.stderr, ".semgrep/walk-open.yml failed its fixture .semgrep/walk-open.py") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// A named JavaScript file is refused: the rule is written for Python only.
func TestJavaScriptIsNotThisGatesLanguage(t *testing.T) {
	got := realRun(t, nil, "extensions/panel.js")
	if got.code != sttrules.ExitCannotRun || !strings.Contains(got.stderr, "extensions/panel.js is not Python") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}
