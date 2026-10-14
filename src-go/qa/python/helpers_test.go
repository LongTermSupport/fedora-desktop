package python

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

const (
	pinnedRuff   = "0.16.8"
	defaultTrees = "# trees\nshell\t.git\npython\t.venv\npython\tuntracked\n"
)

// world is a repository tree on disk plus the fake git, ruff and python3 that look at it.
type world struct {
	t    *testing.T
	root string
	tmp  string
	out  string
	run  *fakeRun
	env  map[string]string
}

// newWorld writes files (relative path to text) under a fresh root with the pin and the trees
// table, unless the files map already names them (an empty text removes it).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		".qa-versions":         "# pins\nRUFF=" + pinnedRuff + "\nSEMGREP=1\n",
		"scripts/qa-trees.tsv": defaultTrees,
		"tool.py":              "print('hi')\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	w := &world{t: t, root: t.TempDir(), tmp: t.TempDir(), out: filepath.Join(t.TempDir(), "report.json"), env: map[string]string{}}
	for rel, text := range all {
		w.write(rel, text)
	}
	w.run = &fakeRun{w: w, ruffVersion: "ruff " + pinnedRuff + "\n"}
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

func (w *world) link(rel, target string) {
	w.t.Helper()
	if err := os.Symlink(target, filepath.Join(w.root, rel)); err != nil {
		w.t.Fatal(err)
	}
	w.run.files = append(w.run.files, rel)
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
		Cwd: w.root, Runner: w.run, TempDir: w.tmp,
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
	Type            string           `json:"type"`
	Status          string           `json:"status"`
	Summary         summary          `json:"summary"`
	Results         []entry          `json:"results"`
	Failures        []entry          `json:"failures"`
	RuffDiagnostics []map[string]any `json:"ruff_diagnostics"`
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

// fakeRun stands in for git, ruff and python3.
type fakeRun struct {
	w     *world
	files []string
	// gitFails is git's exit status when non-zero.
	gitFails int

	ruffVersion    string
	ruffMissing    bool
	ruffVersionRC  int
	pythonMissing  bool
	pythonVersionC int

	// showFiles is what `ruff check --show-files` lists, given the files it was given.
	showFiles func(files []string) []string
	// lint is ruff's answer to a lint run, given the files it was given.
	lint func(files []string) proc.Result
	// compile answers the compile step; nil compiles for real by content (see compileAnswer).
	compile func(files []string) proc.Result

	calls [][]string
	dirs  []string
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c.Argv)
	f.dirs = append(f.dirs, c.Dir)
	switch c.Argv[0] {
	case "git":
		return f.git(c)
	case "ruff":
		return f.ruff(c.Argv[1:])
	case "python3":
		return f.python(c.Argv[1:])
	}
	return proc.Result{}, errors.New("unexpected program " + c.Argv[0])
}

func (f *fakeRun) git(c proc.Cmd) (proc.Result, error) {
	if f.gitFails != 0 {
		return proc.Result{Code: f.gitFails, Stderr: "fatal: not a git repository\n"}, nil
	}
	if slices.Contains(c.Argv, "--deleted") {
		return proc.Result{}, nil
	}
	return proc.Result{Stdout: strings.Join(f.files, "\x00") + "\x00"}, nil
}

func (f *fakeRun) ruff(args []string) (proc.Result, error) {
	if f.ruffMissing {
		return proc.Result{}, errors.New("executable file not found")
	}
	switch {
	case slices.Equal(args, []string{"--version"}):
		return proc.Result{Stdout: f.ruffVersion, Code: f.ruffVersionRC}, nil
	case slices.Contains(args, "--show-files"):
		given := args[slices.Index(args, "--no-cache")+1:]
		if f.showFiles != nil {
			given = f.showFiles(given)
		}
		return proc.Result{Stdout: strings.Join(given, "\n") + "\n"}, nil
	}
	given := args[slices.Index(args, "json")+1:]
	if f.lint != nil {
		return f.lint(given), nil
	}
	return proc.Result{Stdout: "[]\n"}, nil
}

func (f *fakeRun) python(args []string) (proc.Result, error) {
	if f.pythonMissing {
		return proc.Result{}, errors.New("executable file not found")
	}
	if slices.Equal(args, []string{"--version"}) {
		return proc.Result{Stdout: "Python 3.14.0\n", Code: f.pythonVersionC}, nil
	}
	if args[0] != "-I" || args[1] != "-c" {
		return proc.Result{}, errors.New("python3 not run isolated with -c")
	}
	files := args[3:]
	if f.compile != nil {
		return f.compile(files), nil
	}
	return f.compileAnswer(files), nil
}

// compileAnswer answers as the checker would: a file whose text contains SYNTAX_ERROR fails with
// a message that names the file by the path it was given.
func (f *fakeRun) compileAnswer(files []string) proc.Result {
	lines := make([]string, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			f.w.t.Fatalf("the checker was given an unreadable file: %v", err)
		}
		msg := ""
		if strings.Contains(string(data), "SYNTAX_ERROR") {
			msg = "  File \"" + file + "\", line 1\n    SYNTAX_ERROR\nSyntaxError: invalid syntax"
		}
		lines = append(lines, answer(file, msg))
	}
	return proc.Result{Stdout: strings.Join(lines, "\n") + "\n"}
}

func answer(file, msg string) string {
	data, err := json.Marshal(map[string]string{"file": file, "error": msg})
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
