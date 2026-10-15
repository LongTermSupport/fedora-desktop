package patterns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const defaultTrees = "# trees\nshell\t.git\nshell\tuntracked\nshell\t.semgrep\npython\t.venv\n"

// world is a repository tree on disk plus the fake git and semgrep that look at it.
type world struct {
	t    *testing.T
	root string
	tmp  string
	out  string
	run  *fakeRun
	env  map[string]string
}

// newWorld writes files (relative path to text) under a fresh root with the rules, the fixture
// and the trees table, unless the files map already names them (an empty text removes it).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		".semgrep/bash-conventions.yml":  "rules: []\n",
		".semgrep/bash-conventions.bash": "# fixture\n# ruleid: bash-x\n",
		"scripts/qa-trees.tsv":           defaultTrees,
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
	w.run = &fakeRun{w: w}
	w.run.files = w.listAll()
	w.run.scan = func(string) string { return w.run.okReport() }
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

// listAll is every regular file under the root, as git would list a tree with nothing ignored.
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

// report is the machine report the run wrote.
func (w *world) report() map[string]any {
	w.t.Helper()
	data, err := os.ReadFile(w.out)
	if err != nil {
		w.t.Fatalf("no report: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		w.t.Fatalf("report is not JSON: %v", err)
	}
	return m
}

// fakeRun stands in for git and semgrep.
type fakeRun struct {
	w       *world
	files   []string
	deleted []string
	gitCode int
	gitErr  string

	noSemgrep   bool
	versionCode int
	testCode    int
	testOut     string
	scanCode    int
	scanStderr  string
	// scan returns the JSON semgrep writes; "" writes no file.
	scan func(mirror string) string

	calls       [][]string
	dirs        []string
	mirror      string
	mirrorFiles []string
	// mirrorData is each mirror file's bytes as semgrep saw them; ignoreFile is the mirror's
	// .semgrepignore (nil when there is none); rulesBeside is the rules file next to the mirror.
	mirrorData  map[string]string
	ignoreFile  *string
	rulesBeside string
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c.Argv)
	f.dirs = append(f.dirs, c.Dir)
	switch c.Argv[0] {
	case "git":
		return f.git(c)
	case "semgrep":
		return f.semgrep(c)
	}
	return proc.Result{}, fmt.Errorf("unexpected program %q", c.Argv[0])
}

func (f *fakeRun) git(c proc.Cmd) (proc.Result, error) {
	if f.gitCode != 0 {
		return proc.Result{Code: f.gitCode, Stderr: f.gitErr}, nil
	}
	list := f.files
	if slices.Contains(c.Argv, "--deleted") {
		list = f.deleted
	}
	return proc.Result{Stdout: strings.Join(list, "\x00") + "\x00"}, nil
}

func (f *fakeRun) semgrep(c proc.Cmd) (proc.Result, error) {
	if f.noSemgrep {
		return proc.Result{}, errors.New("executable file not found in $PATH")
	}
	switch {
	case slices.Contains(c.Argv, "--version"):
		return proc.Result{Code: f.versionCode, Stdout: "1.0.0\n"}, nil
	case slices.Contains(c.Argv, "--test"):
		return proc.Result{Code: f.testCode, Stdout: f.testOut}, nil
	}
	f.mirror = c.Dir
	f.mirrorFiles = nil
	f.mirrorData = map[string]string{}
	f.ignoreFile = nil
	err := filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(c.Dir, p)
		f.mirrorFiles = append(f.mirrorFiles, rel)
		return relErr
	})
	if err != nil {
		return proc.Result{}, err
	}
	for _, rel := range f.mirrorFiles {
		data, readErr := os.ReadFile(filepath.Join(c.Dir, rel))
		if readErr != nil {
			return proc.Result{}, readErr
		}
		f.mirrorData[rel] = string(data)
	}
	if text, ok := f.mirrorData[".semgrepignore"]; ok {
		f.ignoreFile = &text
		delete(f.mirrorData, ".semgrepignore")
		f.mirrorFiles = slices.DeleteFunc(f.mirrorFiles, func(rel string) bool { return rel == ".semgrepignore" })
	}
	if rules, readErr := os.ReadFile(filepath.Join(c.Dir, "..", "rules.yml")); readErr == nil {
		f.rulesBeside = string(rules)
	}
	slices.Sort(f.mirrorFiles)
	if i := slices.Index(c.Argv, "--json-output"); i >= 0 {
		if body := f.scan(c.Dir); body != "" {
			if err := os.WriteFile(c.Argv[i+1], []byte(body), 0o600); err != nil {
				return proc.Result{}, err
			}
		}
	}
	return proc.Result{Code: f.scanCode, Stderr: f.scanStderr}, nil
}

func (f *fakeRun) okReport() string {
	return semgrepJSON(f.mirrorFiles, nil)
}

func (f *fakeRun) called(prefix ...string) bool {
	return f.callWith(prefix...) != nil
}

func (f *fakeRun) callWith(prefix ...string) []string {
	for _, c := range f.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return c
		}
	}
	return nil
}

// obj and list read a decoded JSON value as an object or an array, failing the test when it is not.
func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %v", v)
	}
	return m
}

func list(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("not an array: %v", v)
	}
	return l
}

// hit is one semgrep match.
type hit struct {
	path, rule, msg string
	line            int
}

// semgrepJSON is the report semgrep writes: what it scanned, what matched, and raw error objects.
func semgrepJSON(scanned []string, results []hit, errs ...string) string {
	res := make([]string, 0, len(results))
	for _, r := range results {
		res = append(res, fmt.Sprintf(`{"check_id":%q,"path":%q,"start":{"line":%d},"extra":{"message":%q}}`, r.rule, r.path, r.line, r.msg))
	}
	sc, err := json.Marshal(scanned)
	if err != nil {
		panic(err)
	}
	if scanned == nil {
		sc = []byte("[]")
	}
	return fmt.Sprintf(`{"results":[%s],"errors":[%s],"paths":{"scanned":%s}}`, strings.Join(res, ","), strings.Join(errs, ","), sc)
}
