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
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/readywait"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(readywait.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(readywait.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// R4: a semgrep that is not installed is named not found by the real process seam.
func TestARealMissingSemgrepIsNamedNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	env := readywait.Env{Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir(), Runner: proc.OS{}}
	if code := readywait.Main(nil, env); code != readywait.ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "ERROR: semgrep not found. Install with: pipx install semgrep") {
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
	code   readywait.ExitCode
	stdout string
}

// realRun runs the gate with the real git and semgrep over a work tree holding the project's own
// rule and fixtures plus files, so the rule file semgrep reads is the one the gate ships.
func realRun(t *testing.T, files map[string]string) outcome {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"scripts/qa-trees.tsv":     "shell\t.semgrep\npython\t.venv\n",
		".semgrep/ready-wait.yml":  projectFile(t, ".semgrep/ready-wait.yml"),
		".semgrep/ready-wait.py":   projectFile(t, ".semgrep/ready-wait.py"),
		".semgrep/ready-wait.bash": projectFile(t, ".semgrep/ready-wait.bash"),
		"scripts/clean.bash":       "#!/usr/bin/env bash\necho hi\n",
		"helpers/clean.py":         "print('hi')\n",
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
	env := readywait.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	code := readywait.Main([]string{"--root", root}, env)
	if code == readywait.ExitCannotRun {
		t.Fatalf("the run could not complete:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), root) {
		t.Fatalf("the checkout path is in the output:\n%s", stdout.String())
	}
	return outcome{code: code, stdout: stdout.String()}
}

const startsServerUnasked = `import subprocess
import time


def start(cmd, ready):
    subprocess.Popen(cmd)
    for _ in range(10):
        time.sleep(0.5)
        if ready():
            return True
    return False
`

// R6, R10-R13, R26: the real semgrep, with the project's own rule, passes a clean tree.
func TestRealSemgrepPassesACleanTree(t *testing.T) {
	got := realRun(t, nil)
	if got.code != readywait.ExitOK {
		t.Fatalf("exit %d\n%s", got.code, got.stdout)
	}
	if !strings.Contains(got.stdout, "passed: 2 failed: 0\n") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

// R13, R25: the real semgrep reports the Python half under the bare rule id, with no "./" and no
// checkout path, and the shell half reports its loop.
func TestRealSemgrepAndTheShellRuleReportBothHalves(t *testing.T) {
	got := realRun(t, map[string]string{
		"helpers/bad.py":   startsServerUnasked,
		"scripts/bad.bash": "#!/usr/bin/env bash\nw &\np=$!\nuntil [ -e r ]; do\n\tsleep 1\ndone\n",
	})
	if got.code != readywait.ExitFailed {
		t.Fatalf("exit %d\n%s", got.code, got.stdout)
	}
	for _, want := range []string{
		"helpers/bad.py:7: ready-wait-ignores-child-exit\n",
		"scripts/bad.bash:4: ready-wait-ignores-child-exit\n",
		"passed: 2 failed: 2\n",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}
