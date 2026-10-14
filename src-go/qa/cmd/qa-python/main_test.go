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
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/python"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(python.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(python.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestWriteReportWritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := writeReport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{}" {
		t.Fatalf("%q %v", data, err)
	}
	if err := writeReport(filepath.Join(t.TempDir(), "no", "r.json"), nil); err == nil {
		t.Fatal("a report in a missing directory was written")
	}
}

// outcome is what one run over a real work tree produced.
type outcome struct {
	code   python.ExitCode
	root   string
	stdout string
	stderr string
	report string
}

// realRun runs the gate with the real git, ruff and python3 over a work tree holding files and a
// pin for the ruff that is installed. A machine without ruff or python3 fails here with the
// gate's own message, as the gate itself would.
func realRun(t *testing.T, files map[string]string) outcome {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	ver, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"ruff", "--version"}})
	if err != nil || ver.Code != 0 {
		t.Fatalf("ruff is needed for this test: %v %+v", err, ver)
	}
	all := map[string]string{
		".qa-versions":         "RUFF=" + strings.Fields(ver.Stdout)[1] + "\n",
		"scripts/qa-trees.tsv": "python\t.venv\n",
		"ruff.toml":            "[lint]\nselect = [\"E4\", \"E7\", \"E9\", \"F\"]\n",
	}
	maps.Copy(all, files)
	for rel, text := range all {
		path := filepath.Join(root, rel)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wrErr := os.WriteFile(path, []byte(text), 0o600); wrErr != nil {
			t.Fatal(wrErr)
		}
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := python.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, TempDir: t.TempDir(), WriteFile: writeReport,
	}
	code := python.Main([]string{"--root", root}, env)
	got := outcome{code: code, root: root, stdout: stdout.String(), stderr: stderr.String()}
	if data, readErr := os.ReadFile(reportPath); readErr == nil {
		got.report = string(data)
	}
	return got
}

// Y14, Y16, Y20: a clean tree passes with the real tools, and the tree is left as it was found.
func TestRealToolsPassACleanTreeAndWriteNothingIntoIt(t *testing.T) {
	got := realRun(t, map[string]string{
		"good.py":             "import os\n\nprint(os.sep)\n",
		"bin/tool":            "#!/usr/bin/env python3\nprint('hi')\n",
		"tpl/h.py.j2":         "#!/usr/bin/env python3\nx = '{{ name }}'\n",
		".venv/lib/broken.py": "def (:\n",
	})
	if got.code != python.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	if want := "✓ python: 2 files OK; 1 rendered template(s) compile (syntax only, not linted)\n"; got.stdout != want {
		t.Fatalf("stdout %q", got.stdout)
	}
	for _, leftover := range []string{"__pycache__", ".ruff_cache", "bin/__pycache__"} {
		if _, err := os.Stat(filepath.Join(got.root, leftover)); err == nil {
			t.Fatalf("the check wrote %s into the tree", leftover)
		}
	}
}

// Y14, Y17, Y18: a syntax error and a lint finding fail with the real tools, without the checkout path.
func TestRealToolsFailASyntaxErrorAndAFinding(t *testing.T) {
	got := realRun(t, map[string]string{
		"bad.py":    "def (:\n",
		"unused.py": "import os\n",
		"good.py":   "print(1)\n",
	})
	if got.code != python.ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"✗ python: bad.py: ", "SyntaxError", "✗ python: ruff: ", "✗ python: failed → "} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	if strings.Contains(got.stdout, got.root) || strings.Contains(got.report, got.root) {
		t.Fatalf("the checkout path is in the output or the report:\n%s\n%s", got.stdout, got.report)
	}
	if !strings.Contains(got.report, `"error": "F401:1 `) || !strings.Contains(got.report, `"filename": "unused.py"`) {
		t.Fatalf("report:\n%s", got.report)
	}
}

// Y15: a file ruff's own exclude rules would drop is refused, not passed.
func TestRealRuffExcludingAFileIsExitTwo(t *testing.T) {
	got := realRun(t, map[string]string{
		"ruff.toml":  "force-exclude = true\nexclude = [\"skipped.py\"]\n[lint]\nselect = [\"F\"]\n",
		"skipped.py": "import os\n",
		"kept.py":    "print(1)\n",
	})
	if got.code != python.ExitCannotRun || !strings.Contains(got.stderr, "ruff did not select 1 of the Python file(s)") || !strings.Contains(got.stderr, "skipped.py") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// Y9: an extensionless Python program with no execute bit is checked, a shell script is not.
func TestRealToolsClassifyByWhatAFileIs(t *testing.T) {
	got := realRun(t, map[string]string{
		"bin/broken": "#!/usr/bin/python3\ndef (:\n",
		"bin/shell":  "#!/usr/bin/bash\ndef (:\n",
		"ok.py":      "print(1)\n",
	})
	if got.code != python.ExitFailed || !strings.Contains(got.stdout, "✗ python: bin/broken: ") || strings.Contains(got.stdout, "bin/shell") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}
