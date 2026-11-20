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
	if code := sttrules.StdinWait.Main([]string{"--help"}, env); code != sttrules.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "usage: qa-stdin-wait ") {
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

// The text a raw read of stdin is spelt with, joined so this file is not itself a finding.
const rawRead = "sys.stdin" + ".read()"

// realRun runs the gate with the real git and semgrep over a work tree holding the project's own
// rule file and fixture plus files, so the rule file semgrep reads is the one the gate ships.
func realRun(t *testing.T, files map[string]string, args ...string) outcome {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"scripts/qa-trees.tsv":    "shell\t.semgrep\npython\t.venv\n",
		".semgrep/stdin-wait.yml": projectFile(t, ".semgrep/stdin-wait.yml"),
		".semgrep/stdin-wait.py":  projectFile(t, ".semgrep/stdin-wait.py"),
		"helpers/clean.py":        "print('hi')\n",
		// The reader itself is the one place a raw read is made, and the rule excludes it.
		"helpers/stdin_text/stdin_text.py": "import sys\ntext = " + rawRead + "\n",
		// So does a finished plan's script.
		"CLAUDE/Plan/Completed/00001-done/probe.py": "import sys\ntext = " + rawRead + "\n",
		"extensions/panel.js":                       "export const a = 1;\n",
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
	code := sttrules.StdinWait.Main(append([]string{"--root", root}, args...), env)
	if strings.Contains(stdout.String()+stderr.String(), root) {
		t.Fatalf("the checkout path is in the output:\n%s%s", stdout.String(), stderr.String())
	}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// The real semgrep, with the project's rule file, passes a clean tree: the reader and the finished
// plan's script are left to the rule's own excludes, not reported as unread.
func TestRealSemgrepPassesACleanTree(t *testing.T) {
	got := realRun(t, nil)
	if got.code != sttrules.ExitOK || !strings.HasSuffix(got.stdout, "passed: 1 failed: 0\n") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// A raw read, in a .py file or an extensionless Python script, is reported under the gate's own
// heading.
func TestRealSemgrepReportsARawReadOfStdin(t *testing.T) {
	raw := "import sys\n\n\ndef main():\n    return " + rawRead + "\n"
	got := realRun(t, map[string]string{"helpers/reads.py": raw, "files/home/.local/bin/tool": "#!/usr/bin/env python3\n" + raw})
	if got.code != sttrules.ExitFailed {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"files/home/.local/bin/tool:6: stdin-read-unbounded\n",
		"helpers/reads.py:5: stdin-read-unbounded\n",
		"stdin-wait rule: 2 finding(s) in 2 file(s).",
		"passed: 1 failed: 2\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

// A file the rule excludes, named, is left out and said so, never refused as unread.
func TestANamedFileTheRuleExcludesIsSaidNotJudged(t *testing.T) {
	got := realRun(t, nil, "helpers/stdin_text/stdin_text.py", "helpers/clean.py")
	if got.code != sttrules.ExitOK ||
		!strings.HasPrefix(got.stdout, "helpers/stdin_text/stdin_text.py: not judged: the rules of .semgrep/stdin-wait.yml exclude it (paths.exclude)\nproved: stdin-read-unbounded") || !strings.HasSuffix(got.stdout, "\npassed: 1 failed: 0\n") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// The rule edited into one its fixture disproves (the json.load form dropped, which no other
// pattern covers) fails its fixture, rather than reporting a clean tree.
func TestRealSemgrepFailsARuleItsFixtureDisproves(t *testing.T) {
	rules := strings.Replace(projectFile(t, ".semgrep/stdin-wait.yml"), "\n      - pattern: json.load(sys.stdin)\n", "\n", 1)
	if rules == projectFile(t, ".semgrep/stdin-wait.yml") {
		t.Fatal("the json.load pattern is not in the rule file; this test edits nothing")
	}
	got := realRun(t, map[string]string{".semgrep/stdin-wait.yml": rules})
	if got.code != sttrules.ExitFailed || !strings.Contains(got.stderr, ".semgrep/stdin-wait.yml failed its fixture .semgrep/stdin-wait.py") {
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
