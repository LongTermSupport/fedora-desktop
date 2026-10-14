package bash

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

const defaultTrees = "# trees\nshell\t.git\nshell\tuntracked\npython\t.venv\n"

// world is a repository tree on disk plus the fake git, bash and shellcheck that look at it.
type world struct {
	t     *testing.T
	root  string
	out   string
	run   *fakeRun
	env   map[string]string
	wrote []string
}

// newWorld writes files (relative path to text) under a fresh root with the trees table, unless
// the files map already names it (an empty text removes the file).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		"scripts/qa-trees.tsv": defaultTrees,
		"tool.bash":            "#!/usr/bin/bash\necho hi\n",
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
	w.run = &fakeRun{w: w}
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

func (w *world) env0(stdout, stderr *bytes.Buffer) Env {
	return Env{
		Getenv: func(k string) string { return w.env[k] }, Stdout: stdout, Stderr: stderr,
		Cwd: w.root, Runner: w.run,
		WriteFile: func(path string, data []byte) error {
			w.wrote = append(w.wrote, path)
			if path != w.out {
				return nil
			}
			return os.WriteFile(path, data, 0o600)
		},
	}
}

func (w *world) main(args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	if _, set := w.env["QA_JSON_OUT"]; !set {
		w.env["QA_JSON_OUT"] = w.out
	}
	code := Main(args, w.env0(&stdout, &stderr))
	return result{code, stdout.String(), stderr.String()}
}

// testReport is the machine report as a test reads it.
type testReport struct {
	Type        string           `json:"type"`
	Status      string           `json:"status"`
	Summary     summary          `json:"summary"`
	Results     []entry          `json:"results"`
	Failures    []entry          `json:"failures"`
	Diagnostics []map[string]any `json:"shellcheck_diagnostics"`
}

// report is the machine report the run wrote.
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

func (w *world) noReport() {
	w.t.Helper()
	if _, err := os.Stat(w.out); err == nil {
		w.t.Fatal("a report was written for a run that could not complete")
	}
}

// fakeRun stands in for git, bash and shellcheck.
type fakeRun struct {
	w *world
	// files is what git would publish; tracked is what it tracks (nil: the same).
	files, tracked []string
	gitFails       int

	shellcheckMissing bool
	shellcheckVersion string
	bashMissing       bool
	// lint is shellcheck's answer to a lint run, given the files it was given.
	lint func(files []string) proc.Result

	// baseList is the suppression allowlist as committed at the merge-base (nil: the base has
	// none); noOriginHead makes origin's default branch unknown.
	baseList     *string
	noOriginHead bool
	// syntaxTimesOut makes every bash -n run out of time.
	syntaxTimesOut bool

	calls []proc.Cmd
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c)
	switch c.Argv[0] {
	case "git":
		return f.git(c)
	case "bash":
		return f.bash(c)
	case "shellcheck":
		return f.shellcheck(c)
	}
	return proc.Result{}, errors.New("unexpected program " + c.Argv[0])
}

func (f *fakeRun) git(c proc.Cmd) (proc.Result, error) {
	if f.gitFails != 0 {
		return proc.Result{Code: f.gitFails, Stderr: "fatal: not a git repository\n"}, nil
	}
	switch {
	case c.Argv[1] == "symbolic-ref":
		if f.noOriginHead {
			return proc.Result{Code: 1}, nil
		}
		return proc.Result{Stdout: "refs/remotes/origin/F44\n"}, nil
	case c.Argv[1] == "merge-base":
		return proc.Result{Stdout: "base0\n"}, nil
	case c.Argv[1] == "ls-tree":
		if f.baseList == nil {
			return proc.Result{}, nil
		}
		return proc.Result{Stdout: c.Argv[len(c.Argv)-1] + "\n"}, nil
	case c.Argv[1] == "show":
		if f.baseList == nil {
			return proc.Result{Code: 128, Stderr: "fatal: path does not exist\n"}, nil
		}
		return proc.Result{Stdout: *f.baseList}, nil
	case slices.Contains(c.Argv, "--deleted"):
		return proc.Result{}, nil
	case !slices.Contains(c.Argv, "--others") && f.tracked != nil:
		return proc.Result{Stdout: strings.Join(f.tracked, "\x00") + "\x00"}, nil
	}
	return proc.Result{Stdout: strings.Join(f.files, "\x00") + "\x00"}, nil
}

// bash answers as `bash -n` does: a file whose text holds SYNTAX_ERROR fails with status 2 and a
// message that names it as it was given, and one holding UNREADABLE with 127.
func (f *fakeRun) bash(c proc.Cmd) (proc.Result, error) {
	if f.bashMissing {
		return proc.Result{}, errors.New("executable file not found")
	}
	if len(c.Argv) == 2 && c.Argv[1] == "--version" {
		return proc.Result{Stdout: "GNU bash, version 5.3.0\n"}, nil
	}
	if len(c.Argv) != 4 || c.Argv[1] != "-n" || c.Argv[2] != "--" {
		return proc.Result{}, errors.New("bash not run as bash -n -- FILE")
	}
	file := c.Argv[3]
	if f.syntaxTimesOut {
		return proc.Result{TimedOut: true, Code: -1}, nil
	}
	data, err := os.ReadFile(filepath.Join(c.Dir, file))
	if err != nil {
		f.w.t.Fatalf("bash -n was given a file it cannot read from %q: %v", c.Dir, err)
	}
	switch {
	case strings.Contains(string(data), "SYNTAX_ERROR"):
		return proc.Result{Stdout: file + ": line 2: syntax error near unexpected token `fi'\n", Code: 2}, nil
	case strings.Contains(string(data), "UNREADABLE"):
		return proc.Result{Stdout: file + ": No such file or directory\n", Code: 127}, nil
	}
	return proc.Result{}, nil
}

func (f *fakeRun) shellcheck(c proc.Cmd) (proc.Result, error) {
	if f.shellcheckMissing {
		return proc.Result{}, errors.New("executable file not found")
	}
	if len(c.Argv) == 2 && c.Argv[1] == "--version" {
		out := f.shellcheckVersion
		if out == "" {
			out = "ShellCheck - shell script analysis tool\nversion: 0.11.0\nlicense: GPL\n"
		}
		return proc.Result{Stdout: out}, nil
	}
	given := c.Argv[slices.Index(c.Argv, "--")+1:]
	if f.lint != nil {
		return f.lint(given), nil
	}
	return proc.Result{Stdout: "[]\n"}, nil
}

// calls to a program, whole commands.
func (f *fakeRun) cmds(name string) []proc.Cmd {
	var out []proc.Cmd
	for _, c := range f.calls {
		if c.Argv[0] == name {
			out = append(out, c)
		}
	}
	return out
}

// finding is a shellcheck JSON diagnostic.
func finding(file string, line int, level string, code int, msg string) map[string]any {
	return map[string]any{"file": file, "line": line, "endLine": line, "column": 1, "endColumn": 2, "level": level, "code": code, "message": msg, "fix": nil}
}

// lintAnswer is what shellcheck prints for diagnostics, with status 1 when there are any.
func lintAnswer(diags ...map[string]any) proc.Result {
	data, err := json.Marshal(diags)
	if err != nil {
		panic(err)
	}
	res := proc.Result{Stdout: string(data) + "\n"}
	if len(diags) > 0 {
		res.Code = 1
	}
	return res
}
