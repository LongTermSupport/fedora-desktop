package sttrules

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// rulesText is a rule file shaped like the real one: two rules for JavaScript, one for Python,
// one id used in both languages.
const rulesText = `rules:
  # a comment between rules
  - id: model-present-without-weights
    languages: [javascript]
    pattern-regex: 'snap\w+'
    message: m
    severity: ERROR
  - id: model-present-without-weights
    languages: [python]
    pattern-regex: 'snap\w+'
    message: m
    severity: ERROR
  - id: dropdown-label-carries-explanation
    languages: [javascript]
    pattern-regex: '(?s)^.(?:.*(?:—|\(|\)).*|.{25,}).$'
    message: m
    severity: ERROR
`

const (
	jsFixtureText = "// ruleid: model-present-without-weights\nx\n// ruleid: dropdown-label-carries-explanation\ny\n"
	pyFixtureText = "# ruleid: model-present-without-weights\nx = 1\n"
)

// fakeRunner answers semgrep from its fields and runs the real git, which only lists files.
type fakeRunner struct {
	version   proc.Result
	versionEr error
	// test answers each self-test; testFor, when set, answers by fixture path.
	test    proc.Result
	testFor func(fixture string) proc.Result
	testEr  error
	// scan answers each scan, given the language (js or py) judged from its rule file.
	scan func(lang string, files []string) proc.Result

	calls   [][]string
	dirs    []string
	configs []scanConfig
}

// scanConfig is what a scan was given, read at the moment semgrep was called.
type scanConfig struct {
	path  string
	text  string
	mode  os.FileMode
	files []string
	argv  []string
}

func (f *fakeRunner) Run(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	if cmd.Argv[0] == "git" {
		return proc.OS{}.Run(ctx, cmd)
	}
	f.calls = append(f.calls, cmd.Argv)
	f.dirs = append(f.dirs, cmd.Dir)
	switch {
	case slices.Contains(cmd.Argv, "--version"):
		return f.version, f.versionEr
	case slices.Contains(cmd.Argv, "--test"):
		if f.testFor != nil {
			return f.testFor(cmd.Argv[len(cmd.Argv)-1]), f.testEr
		}
		return f.test, f.testEr
	}
	cfg := scanConfig{argv: cmd.Argv, files: cmd.Argv[slices.Index(cmd.Argv, "--max-target-bytes=0")+1:]}
	cfg.path = cmd.Argv[slices.Index(cmd.Argv, "--config")+1]
	data, err := os.ReadFile(cfg.path)
	if err != nil {
		return proc.Result{}, err
	}
	cfg.text = string(data)
	info, err := os.Stat(cfg.path)
	if err != nil {
		return proc.Result{}, err
	}
	cfg.mode = info.Mode().Perm()
	f.configs = append(f.configs, cfg)
	lang := "py"
	if strings.Contains(cfg.text, "dropdown-label-carries-explanation") {
		lang = "js"
	}
	return f.scan(lang, cfg.files), nil
}

// scans is the argv of each scan run (not the version or the self-test).
func (f *fakeRunner) scans() [][]string {
	var out [][]string
	for _, argv := range f.calls {
		if !slices.Contains(argv, "--version") && !slices.Contains(argv, "--test") {
			out = append(out, argv)
		}
	}
	return out
}

func (f *fakeRunner) tests() [][]string {
	var out [][]string
	for _, argv := range f.calls {
		if slices.Contains(argv, "--test") {
			out = append(out, argv)
		}
	}
	return out
}

type jsonResult = semgrepreport.Result

// hit is a result at a path and line.
func hit(path string, line int) jsonResult {
	r := jsonResult{CheckID: "model-present-without-weights", Path: path}
	r.Start.Line = line
	return r
}

// reportFor is semgrep's JSON naming scanned files, with these results and error entries.
func reportFor(t *testing.T, scanned []string, results []jsonResult, errs []map[string]string) proc.Result {
	t.Helper()
	data, err := json.Marshal(map[string]any{"results": results, "errors": errs, "paths": map[string]any{"scanned": scanned}})
	if err != nil {
		t.Fatal(err)
	}
	return proc.Result{Stdout: string(data)}
}

type world struct {
	t      *testing.T
	root   string
	fake   *fakeRunner
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// newWorld writes a repository under a fresh root and tracks it in git. files overrides or adds
// files; an empty text removes one.
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root, fake: &fakeRunner{version: proc.Result{Stdout: "1.0\n"}}}
	w.fake.scan = func(_ string, files []string) proc.Result { return reportFor(t, files, nil, nil) }
	all := map[string]string{
		ruleFile:                  rulesText,
		jsFixtureFile:             jsFixtureText,
		pyFixtureFile:             pyFixtureText,
		"scripts/qa-trees.tsv":    "shell\t.semgrep\npython\t.venv\n",
		"extensions/panel.js":     "export const a = 1;\n",
		"helpers/mod.py":          "print('hi')\n",
		".semgrep/unrelated.py":   "print('fixture')\n",
		".claude/skills/s/run.py": "print('vendored')\n",
		"node_modules/p/i.js":     "module.exports = 1;\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	for rel, text := range all {
		w.write(rel, text)
	}
	w.track()
	return w
}

// track makes git track every file written so far.
func (w *world) track() {
	w.t.Helper()
	for _, argv := range [][]string{{"git", "init", "-q"}, {"git", "add", "-A", "-f"}} {
		if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: argv, Dir: w.root}); err != nil || res.Code != 0 {
			w.t.Fatalf("%v: %v %+v", argv, err, res)
		}
	}
}

func (w *world) write(rel, text string) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) env() Env {
	return Env{Stdout: &w.stdout, Stderr: &w.stderr, Cwd: w.root, Runner: w.fake}
}

func (w *world) run(args ...string) ExitCode {
	w.t.Helper()
	return Main(append([]string{"--root", w.root}, args...), w.env())
}

func (w *world) wantExit(code ExitCode, got ExitCode) {
	w.t.Helper()
	if got != code {
		w.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, code, w.stdout.String(), w.stderr.String())
	}
}

func (w *world) wantStderr(parts ...string) {
	w.t.Helper()
	for _, p := range parts {
		if !strings.Contains(w.stderr.String(), p) {
			w.t.Fatalf("stderr lacks %q:\n%s", p, w.stderr.String())
		}
	}
}

func (w *world) wantStdout(parts ...string) {
	w.t.Helper()
	for _, p := range parts {
		if !strings.Contains(w.stdout.String(), p) {
			w.t.Fatalf("stdout lacks %q:\n%s", p, w.stdout.String())
		}
	}
}

// handedFiles is every file handed to a scan, across languages, sorted.
func (f *fakeRunner) handedFiles() []string {
	var all []string
	for _, c := range f.configs {
		all = append(all, c.files...)
	}
	slices.Sort(all)
	return all
}
