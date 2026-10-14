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
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/js"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(js.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(js.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

func TestWriteReportWritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := writeReport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Clean(path)); err != nil || string(data) != "{}" {
		t.Fatalf("%q %v", data, err)
	}
	if err := writeReport(filepath.Join(t.TempDir(), "no", "r.json"), nil); err == nil {
		t.Fatal("a report in a missing directory was written")
	}
}

// repoExtensions finds the checkout's extensions/ directory by walking up from the test's
// directory; its node_modules holds the real ESLint, and its config is the real ruleset.
func repoExtensions(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "extensions")
		if _, statErr := os.Stat(filepath.Join(candidate, "node_modules", ".bin", "eslint")); statErr == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("the checkout's extensions/node_modules (cd extensions && npm ci) is needed for this test")
		}
		dir = parent
	}
}

// outcome is what one run over a real work tree produced.
type outcome struct {
	code   js.ExitCode
	root   string
	stdout string
	stderr string
	report string
}

// realRun runs the gate with the real git, node and ESLint over a work tree holding files. The
// tree carries the checkout's real .eslintrc.json and a link to its node_modules, unless
// withoutEslint is set, which leaves the link out.
func realRun(t *testing.T, files map[string]string, withoutEslint bool) outcome {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	ext := repoExtensions(t)
	config, err := os.ReadFile(filepath.Clean(filepath.Join(ext, ".eslintrc.json")))
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]string{
		"scripts/qa-trees.tsv":      "shell\t.git\nshell\tnode_modules\npython\t.venv\n",
		"extensions/.eslintrc.json": string(config),
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
	if !withoutEslint {
		if linkErr := os.Symlink(filepath.Join(ext, "node_modules"), filepath.Join(root, "extensions", "node_modules")); linkErr != nil {
			t.Fatal(linkErr)
		}
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	env := js.Env{
		Getenv: func(k string) string {
			if k == "QA_JSON_OUT" {
				return reportPath
			}
			return ""
		},
		Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}, WriteFile: writeReport,
	}
	code := js.Main([]string{"--root", root}, env)
	got := outcome{code: code, root: root, stdout: stdout.String(), stderr: stderr.String()}
	if data, readErr := os.ReadFile(filepath.Clean(reportPath)); readErr == nil {
		got.report = string(data)
	}
	return got
}

// J14: a clean tree passes with the real tools, and the tree is left as it was found.
func TestRealToolsPassACleanTreeAndWriteNothingIntoIt(t *testing.T) {
	got := realRun(t, map[string]string{
		"extensions/ext@x/extension.js":               "export const a = 1;\n",
		"tests/extensions/t.mjs":                      "export const b = 2;\n",
		"extensions/ext@x/node_modules/dep/broken.js": "def (:\n",
	}, false)
	if got.code != js.ExitOK || got.stdout != "✓ js: 2 files OK (1 linted by eslint)\n" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, leftover := range []string{".eslintcache", "extensions/.eslintcache"} {
		if _, err := os.Stat(filepath.Join(got.root, leftover)); err == nil {
			t.Fatalf("the check wrote %s into the tree", leftover)
		}
	}
}

// J7, J12, J13: a syntax error and a lint finding fail with the real tools, without the checkout path.
func TestRealToolsFailASyntaxErrorAndALintFinding(t *testing.T) {
	got := realRun(t, map[string]string{
		"tests/extensions/bad.mjs":      "export const = ;\n",
		"extensions/ext@x/extension.js": "export const a = missingThing();\n",
	}, false)
	if got.code != js.ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"✗ js: tests/extensions/bad.mjs: ", "SyntaxError", "✗ js: extensions/ext@x/extension.js:1:", "(no-undef)", "✗ js: 2 failing file(s) of 2 → "} {
		if !strings.Contains(got.stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
	if strings.Contains(got.stdout, got.root) || strings.Contains(got.report, got.root) {
		t.Fatalf("the checkout path is in the output or the report:\n%s\n%s", got.stdout, got.report)
	}
}

// J9: a nested config that switches the rule off, and an eslint-disable comment, hide nothing.
func TestRealNestedConfigAndInlineDisableCannotWeakenTheRules(t *testing.T) {
	got := realRun(t, map[string]string{
		"extensions/ext@x/.eslintrc.json": "{\"root\": true, \"rules\": {\"no-undef\": \"off\"}}\n",
		"extensions/ext@x/nested.js":      "export const a = nestedMissing();\n",
		"extensions/ext@x/inline.js":      "/* eslint-disable */\nexport const b = inlineMissing();\n",
		"extensions/ext@x/line.js":        "export const c = lineMissing(); // eslint-disable-line no-undef\n",
	}, false)
	if got.code != js.ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
	for _, name := range []string{"nested.js", "inline.js", "line.js"} {
		if !strings.Contains(got.stdout, "✗ js: extensions/ext@x/"+name+":") {
			t.Fatalf("%s was not failed:\n%s", name, got.stdout)
		}
	}
}

// J4: without the tooling the run is exit 2, naming the setup, and nothing is reported.
func TestRealMissingEslintToolingIsExitTwo(t *testing.T) {
	got := realRun(t, map[string]string{"extensions/ext@x/extension.js": "export const a = 1;\n"}, true)
	if got.code != js.ExitCannotRun || !strings.Contains(got.stderr, "npm ci") || got.stdout != "" || got.report != "" {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

// closedStdoutChild names the environment variable that turns this test binary into the command.
const closedStdoutChild = "QA_JS_CLOSED_STDOUT_CHILD"

// J2: with stdout closed (>&-) the run is exit 2, it does not report a verdict nobody can read.
// The test re-runs its own binary as the command, with fd 1 closed by the shell.
func TestAClosedStdoutIsExitTwo(t *testing.T) {
	if args := os.Getenv(closedStdoutChild); args != "" {
		os.Exit(run(strings.Fields(args)))
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, runErr := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"bash", "-c", `exec "$0" -test.run='^TestAClosedStdoutIsExitTwo$' >&-`, self},
		Env:  []string{closedStdoutChild + "=--root " + t.TempDir(), "QA_JSON_OUT=" + filepath.Join(t.TempDir(), "r.json")},
	})
	if runErr != nil || res.Code != int(js.ExitCannotRun) || !strings.Contains(res.Stderr, "ERROR: stdout is closed") {
		t.Fatalf("closed stdout: %v exit %d\nstderr:\n%s", runErr, res.Code, res.Stderr)
	}
}
