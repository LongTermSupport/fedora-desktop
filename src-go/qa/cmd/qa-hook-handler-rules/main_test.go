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
	if code := sttrules.HookHandlers.Main([]string{"--help"}, env); code != sttrules.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "usage: qa-hook-handler-rules ") {
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
		"scripts/qa-trees.tsv":       "shell\t.semgrep\npython\t.venv\n",
		".semgrep/hook-handlers.yml": projectFile(t, ".semgrep/hook-handlers.yml"),
		".semgrep/hook-handlers.py":  projectFile(t, ".semgrep/hook-handlers.py"),
		"helpers/clean.py":           "print('hi')\n",
		"extensions/panel.js":        "export const a = 1;\n",
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
	code := sttrules.HookHandlers.Main(append([]string{"--root", root}, args...), env)
	if strings.Contains(stdout.String()+stderr.String(), root) {
		t.Fatalf("the checkout path is in the output:\n%s%s", stdout.String(), stderr.String())
	}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

const cwdHandler = `import os

from claude_code_hooks_daemon.core import Handler


class Bad(Handler):
    def matches(self, file_path):
        return os.path.realpath(file_path).startswith("/etc")
`

// The real semgrep, with the project's rule file, passes a clean tree and reads only Python.
func TestRealSemgrepPassesACleanTree(t *testing.T) {
	got := realRun(t, nil)
	if got.code != sttrules.ExitOK || got.stdout != "passed: 1 failed: 0\n" {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// A handler that resolves a path against the daemon's working directory is reported under the
// gate's own heading.
func TestRealSemgrepReportsAHandlerResolvingAgainstTheCwd(t *testing.T) {
	got := realRun(t, map[string]string{".claude/hooks/handlers/bad.py": cwdHandler})
	if got.code != sttrules.ExitFailed {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		".claude/hooks/handlers/bad.py:8: hook-path-resolved-against-cwd\n",
		"hook handler rules: 1 finding(s) in 1 file(s).",
		"passed: 1 failed: 1\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

// The rule edited into matching nothing fails its fixture, rather than reporting a clean tree.
func TestRealSemgrepFailsARuleThatMatchesNothing(t *testing.T) {
	rules := strings.ReplaceAll(projectFile(t, ".semgrep/hook-handlers.yml"), "claude_code_hooks_daemon", "no_such_daemon")
	got := realRun(t, map[string]string{".semgrep/hook-handlers.yml": rules})
	if got.code != sttrules.ExitFailed || !strings.Contains(got.stderr, ".semgrep/hook-handlers.yml failed its fixture .semgrep/hook-handlers.py") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// A JavaScript rule has no place in this rule file, and a named JavaScript file is refused.
func TestJavaScriptIsNotThisGatesLanguage(t *testing.T) {
	rules := projectFile(t, ".semgrep/hook-handlers.yml") + "  - id: js\n    languages: [javascript]\n    pattern: x\n    message: x\n    severity: ERROR\n"
	got := realRun(t, map[string]string{".semgrep/hook-handlers.yml": rules})
	if got.code != sttrules.ExitCannotRun || !strings.Contains(got.stderr, `rule js: language "javascript" is not python`) {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	got = realRun(t, nil, "extensions/panel.js")
	if got.code != sttrules.ExitCannotRun || !strings.Contains(got.stderr, "extensions/panel.js is not Python") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}
