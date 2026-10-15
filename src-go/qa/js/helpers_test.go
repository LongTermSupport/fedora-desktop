package js

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const defaultTrees = "# trees\nshell\t.git\nshell\tnode_modules\nshell\tuntracked\npython\t.venv\n"

// world is a repository tree on disk plus the fake git, node and eslint that look at it.
type world struct {
	t    *testing.T
	root string
	out  string
	run  *fakeRun
	env  map[string]string
}

// newWorld writes files (relative path to text) under a fresh root with the trees table, the
// ESLint config and binary, and two JavaScript files, unless the files map already names them
// (an empty text removes it).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		"scripts/qa-trees.tsv":                     defaultTrees,
		"extensions/.eslintrc.json":                "{}\n",
		"extensions/node_modules/.bin/eslint":      "#!/usr/bin/env node\n",
		"extensions/ext@x/extension.js":            "export const a = 1;\n",
		"tests/extensions/test-thing.mjs":          "export const b = 2;\n",
		"extensions/ext@x/node_modules/dep/dep.js": "x y z\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	w := &world{t: t, root: t.TempDir(), out: filepath.Join(t.TempDir(), "report.json"), env: map[string]string{}}
	for rel, text := range all {
		w.write(rel, text)
	}
	w.run = &fakeRun{w: w, nodeVersion: "v22.1.0\n", eslintVersion: "v8.57.1\n"}
	w.run.files = w.listAll()
	return w
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

// eslintPath is where the gate must find ESLint.
func (w *world) eslintPath() string {
	return filepath.Join(w.root, "extensions", "node_modules", ".bin", "eslint")
}

// listAll is every file under the root, as git would list a tree with nothing ignored.
func (w *world) listAll() []string {
	var files []string
	err := filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(w.root, p)
		files = append(files, rel)
		return relErr
	})
	if err != nil {
		w.t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

// result is one run of Main.
type result struct {
	code           ExitCode
	stdout, stderr string
}

func (w *world) main(args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Getenv: func(k string) string { return w.env[k] }, Stdout: &stdout, Stderr: &stderr,
		Cwd: w.root, Runner: w.run,
		WriteFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
	}
	if _, set := w.env["QA_JSON_OUT"]; !set {
		w.env["QA_JSON_OUT"] = w.out
	}
	code := Main(args, env)
	return result{code, stdout.String(), stderr.String()}
}

// testReport is the machine report as a test reads it.
type testReport struct {
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Summary  summary `json:"summary"`
	Results  []entry `json:"results"`
	Failures []entry `json:"failures"`
	Eslint   struct {
		Ran    bool   `json:"ran"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"eslint"`
}

func (w *world) report() testReport {
	w.t.Helper()
	data, err := os.ReadFile(w.out)
	if err != nil {
		w.t.Fatalf("no report: %v", err)
	}
	var rep testReport
	if err := json.Unmarshal(data, &rep); err != nil {
		w.t.Fatalf("report is not JSON: %v", err)
	}
	return rep
}

func (w *world) rawReport() string {
	w.t.Helper()
	data, err := os.ReadFile(w.out)
	if err != nil {
		w.t.Fatalf("no report: %v", err)
	}
	return string(data)
}

func (w *world) noReport() {
	w.t.Helper()
	if _, err := os.Stat(w.out); err == nil {
		w.t.Fatal("a report was written for a run that could not complete")
	}
}

// fakeRun stands in for git, node and ESLint.
type fakeRun struct {
	w     *world
	files []string
	// gitFails is git's exit status when non-zero.
	gitFails int

	nodeVersion   string
	nodeMissing   bool
	eslintVersion string
	// nodeCheck answers a parse check of one file; nil fails a file holding SYNTAX_ERROR.
	nodeCheck func(file string) (proc.Result, error)
	// lint answers an ESLint run given the files; nil reports LINT_ERROR files.
	lint func(files []string) (proc.Result, error)

	calls [][]string
	dirs  []string
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c.Argv)
	f.dirs = append(f.dirs, c.Dir)
	switch {
	case c.Argv[0] == "git":
		return f.git(c.Argv)
	case c.Argv[0] == "node":
		return f.node(c.Argv[1:])
	case c.Argv[0] == f.w.eslintPath():
		return f.eslint(c.Argv[1:])
	}
	return proc.Result{}, errors.New("unexpected program " + c.Argv[0])
}

func (f *fakeRun) git(argv []string) (proc.Result, error) {
	if f.gitFails != 0 {
		return proc.Result{Code: f.gitFails, Stderr: "fatal: not a git repository\n"}, nil
	}
	if slices.Contains(argv, "--deleted") {
		return proc.Result{}, nil
	}
	return proc.Result{Stdout: strings.Join(f.files, "\x00") + "\x00"}, nil
}

func (f *fakeRun) node(args []string) (proc.Result, error) {
	if f.nodeMissing {
		return proc.Result{}, errors.New("executable file not found")
	}
	if slices.Equal(args, []string{"--version"}) {
		return proc.Result{Stdout: f.nodeVersion}, nil
	}
	if len(args) != 2 || args[0] != "--check" {
		return proc.Result{}, errors.New("node not run as --check FILE: " + strings.Join(args, " "))
	}
	if f.nodeCheck != nil {
		return f.nodeCheck(args[1])
	}
	data, err := os.ReadFile(filepath.Clean(args[1]))
	if err != nil {
		f.w.t.Fatalf("node was given an unreadable file: %v", err)
	}
	if strings.Contains(string(data), "SYNTAX_ERROR") {
		return proc.Result{Code: 1, Stderr: args[1] + ":1\nSYNTAX_ERROR\n^^^^^^^^^^^^\n\nSyntaxError: Unexpected identifier\n\nNode.js v22.1.0\n"}, nil
	}
	return proc.Result{}, nil
}

func (f *fakeRun) eslint(args []string) (proc.Result, error) {
	if slices.Equal(args, []string{"--version"}) {
		return proc.Result{Stdout: f.eslintVersion}, nil
	}
	i := slices.Index(args, "json")
	if i < 0 {
		return proc.Result{}, errors.New("eslint was not asked for json")
	}
	files := args[i+1:]
	if f.lint != nil {
		return f.lint(files)
	}
	var results []map[string]any
	code := 0
	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			f.w.t.Fatalf("eslint was given an unreadable file: %v", err)
		}
		msgs := []map[string]any{}
		if strings.Contains(string(data), "LINT_ERROR") {
			msgs = append(msgs, map[string]any{"ruleId": "no-undef", "severity": 2, "message": "'LINT_ERROR' is not defined.", "line": 1, "column": 8})
			code = 1
		}
		results = append(results, map[string]any{"filePath": file, "messages": msgs})
	}
	return proc.Result{Stdout: mustJSON(results), Code: code}, nil
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// argv is the calls whose program is name.
func (f *fakeRun) argv(name string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == name {
			out = append(out, c)
		}
	}
	return out
}
