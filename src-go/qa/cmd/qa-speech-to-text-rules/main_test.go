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
	if code := run([]string{"--help"}); code != int(sttrules.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(sttrules.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// S4: a semgrep that is not installed is named not found by the real process seam.
func TestARealMissingSemgrepIsNamedNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	env := sttrules.Env{Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir(), Runner: proc.OS{}}
	if code := sttrules.Main(nil, env); code != sttrules.ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)") {
		t.Fatalf("stderr:\n%s", stderr.String())
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
// rules and fixtures plus files, so the rule file semgrep reads is the one the gate ships.
func realRun(t *testing.T, files map[string]string, args ...string) outcome {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"scripts/qa-trees.tsv":        "shell\t.semgrep\npython\t.venv\n",
		".semgrep/speech-to-text.yml": projectFile(t, ".semgrep/speech-to-text.yml"),
		".semgrep/speech-to-text.js":  projectFile(t, ".semgrep/speech-to-text.js"),
		".semgrep/speech-to-text.py":  projectFile(t, ".semgrep/speech-to-text.py"),
		"extensions/clean.js":         "export const a = 1;\n",
		"helpers/clean.py":            "print('hi')\n",
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
	code := sttrules.Main(append([]string{"--root", root}, args...), env)
	if strings.Contains(stdout.String()+stderr.String(), root) {
		t.Fatalf("the checkout path is in the output:\n%s%s", stdout.String(), stderr.String())
	}
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

const weightlessPython = `def installed(model_dir):
    snapshots = model_dir / 'snapshots'
    return snapshots.exists()
`

const explainingDropdown = `class Panel {
    build() {
        this._addComboRow(g, s, 'k', 't', 'sub', ['a'], ['Standard — load then start (~3-6s)']);
    }
}
`

// S8, S11, S12, S17: the real semgrep, with the project's own rules, passes a clean tree.
func TestRealSemgrepPassesACleanTree(t *testing.T) {
	got := realRun(t, nil)
	if got.code != sttrules.ExitOK || got.stdout != "passed: 2 failed: 0\n" {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// S11, S16: the real semgrep reports both rules, in both languages, under the bare rule id.
func TestRealSemgrepReportsBothRulesInBothLanguages(t *testing.T) {
	got := realRun(t, map[string]string{
		"helpers/bad.py":         weightlessPython,
		"extensions/dropdown.js": explainingDropdown,
	})
	if got.code != sttrules.ExitFailed {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"helpers/bad.py:2: model-present-without-weights\n",
		"extensions/dropdown.js:3: dropdown-label-carries-explanation\n",
		"passed: 2 failed: 2\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

// S11: an extensionless Python script is judged with the Python rules only; the JavaScript rules
// are not applied to it, which semgrep would do under --scan-unknown-extensions given them all.
func TestRealSemgrepJudgesAnExtensionlessPythonScriptWithPythonRulesOnly(t *testing.T) {
	got := realRun(t, map[string]string{"files/bin/stt-tool": "#!/usr/bin/env python3\n" + weightlessPython})
	if got.code != sttrules.ExitFailed || !strings.Contains(got.stdout, "files/bin/stt-tool:3: model-present-without-weights\n") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if strings.Contains(got.stdout, "dropdown-label") {
		t.Fatalf("a JavaScript rule ran on a Python script:\n%s", got.stdout)
	}
}

// S8: a rule edited into matching nothing fails its fixture, rather than reporting a clean tree.
func TestRealSemgrepFailsARuleThatMatchesNothing(t *testing.T) {
	rules := strings.ReplaceAll(projectFile(t, ".semgrep/speech-to-text.yml"), `snapshots\b`, `nothing-like-this\b`)
	got := realRun(t, map[string]string{".semgrep/speech-to-text.yml": rules})
	if got.code != sttrules.ExitFailed || !strings.Contains(got.stderr, "failed its fixture") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if got.stdout != "" {
		t.Fatalf("stdout %q", got.stdout)
	}
}

// S7: a fixture with no annotations cannot pass for a proof, with the real semgrep either.
func TestRealSemgrepRefusesAFixtureWithNoAnnotations(t *testing.T) {
	got := realRun(t, map[string]string{".semgrep/speech-to-text.py": "x = 1\n"})
	if got.code != sttrules.ExitCannotRun || !strings.Contains(got.stderr, "marks no ruleid line") {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

// S10: named files are the only ones judged.
func TestRealSemgrepJudgesOnlyTheNamedFiles(t *testing.T) {
	got := realRun(t, map[string]string{"helpers/bad.py": weightlessPython}, "helpers/clean.py")
	if got.code != sttrules.ExitOK || got.stdout != "passed: 1 failed: 0\n" {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	got = realRun(t, map[string]string{"helpers/bad.py": weightlessPython}, "helpers/bad.py")
	if got.code != sttrules.ExitFailed {
		t.Fatalf("exit %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}
