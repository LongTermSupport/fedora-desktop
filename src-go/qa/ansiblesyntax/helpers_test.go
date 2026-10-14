package ansiblesyntax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	defaultTrees = "# trees\nshell\t.git\nshell\tuntracked\npython\t.venv\n"
	playbookText = "---\n- hosts: localhost\n  tasks: []\n"
	// basePlaybook is in every world: the shared discovery refuses a tree with no tracked
	// playbook under playbooks/.
	basePlaybook = "playbooks/base.yml"
	// passwordSetting is the name ansible-config gives a vault password file.
	passwordSetting = "DEFAULT_VAULT_PASSWORD_FILE"
)

// world is a repository tree on disk plus the fake git and ansible that look at it.
type world struct {
	t     *testing.T
	root  string
	out   string
	run   *fakeRun
	env   map[string]string
	wrote []string
}

// newWorld writes files (relative path to text) under a fresh root with the trees table and an
// ansible.cfg, unless files names them (an empty text removes it).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		"scripts/qa-trees.tsv": defaultTrees,
		"ansible.cfg":          "[defaults]\n",
		basePlaybook:           playbookText,
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
	w.run = &fakeRun{w: w, configDump: "[]"}
	w.run.tracked = w.listAll()
	w.run.published = w.run.tracked
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

// addPlaybook writes a playbook that parses and makes git list it.
func (w *world) addPlaybook(rel string) {
	w.t.Helper()
	w.addFile(rel, playbookText)
}

// addFile writes a file and makes git list it, tracked and published.
func (w *world) addFile(rel, text string) {
	w.t.Helper()
	w.write(rel, text)
	w.run.tracked = append(w.run.tracked, rel)
	w.run.published = append(w.run.published, rel)
}

// listAll is every file under the root.
func (w *world) listAll() []string {
	var files []string
	err := filepath.WalkDir(w.root, func(p string, d os.DirEntry, err error) error {
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

// setting makes ansible-config report one setting.
func (w *world) setting(name string, value any) {
	w.t.Helper()
	data, err := json.Marshal([]map[string]any{{"name": name, "value": value}, {"GALAXY_SERVERS": map[string]any{}}})
	if err != nil {
		w.t.Fatal(err)
	}
	w.run.configDump = string(data)
}

// result is one run of Main.
type result struct {
	code           ExitCode
	stdout, stderr string
}

// failingWriter is a stream that cannot be written to.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func (w *world) environment(stdout, stderr *bytes.Buffer) Env {
	if _, set := w.env["QA_JSON_OUT"]; !set {
		w.env["QA_JSON_OUT"] = w.out
	}
	return Env{
		Getenv: func(k string) string { return w.env[k] }, Stdout: stdout, Stderr: stderr,
		Cwd: w.root, Runner: w.run,
		WriteFile: func(path string, data []byte) error {
			w.wrote = append(w.wrote, path)
			return os.WriteFile(path, data, 0o600)
		},
	}
}

func (w *world) main(args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, w.environment(&stdout, &stderr))
	return result{code, stdout.String(), stderr.String()}
}

// testReport is the machine report as a test reads it.
type testReport struct {
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Summary  summary `json:"summary"`
	Results  []entry `json:"results"`
	Failures []entry `json:"failures"`
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
	if rep.Failures == nil {
		w.t.Fatal("failures is null, not an array")
	}
	return rep
}

func (w *world) noReport() {
	w.t.Helper()
	if _, err := os.Stat(w.out); err == nil {
		w.t.Fatal("a report was written for a run that could not complete")
	}
}

// fakeRun stands in for git, ansible-config and ansible-playbook. Its engine fails a playbook
// whose text holds SYNTAX_ERROR, and is safe for the parallel checks.
type fakeRun struct {
	w *world
	// tracked and published are what git lists without and with --others.
	tracked, published []string

	// configDump is what ansible-config prints.
	configDump  string
	configCode  int
	missing     string
	versionCode int
	// The playbook checks that go wrong, each by the path suffix it applies to.
	startFails, timesOut, leftover, silentFail string
	// barrier, when set, holds each check until that many are running at once.
	barrier      *sync.WaitGroup
	barrierSlots int

	mu      sync.Mutex
	calls   [][]string
	cmds    []proc.Cmd
	running int
	peak    int
}

func (f *fakeRun) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c.Argv)
	f.cmds = append(f.cmds, c)
	f.mu.Unlock()
	if c.Argv[0] == f.missing {
		return proc.Result{}, errors.New("executable file not found")
	}
	switch c.Argv[0] {
	case "git":
		if slices.Contains(c.Argv, "--deleted") {
			return proc.Result{}, nil
		}
		files := f.tracked
		if slices.Contains(c.Argv, "--others") {
			files = f.published
		}
		return proc.Result{Stdout: strings.Join(files, "\x00") + "\x00"}, nil
	case "ansible-config":
		return proc.Result{Stdout: f.configDump, Code: f.configCode}, nil
	case "ansible-playbook":
		return f.ansible(c)
	}
	return proc.Result{}, errors.New("unexpected program " + c.Argv[0])
}

func (f *fakeRun) ansible(c proc.Cmd) (proc.Result, error) {
	if slices.Equal(c.Argv[1:], []string{"--version"}) {
		return proc.Result{Stdout: "ansible-playbook [core 2.19]\n", Code: f.versionCode}, nil
	}
	file := c.Argv[len(c.Argv)-1]
	f.enter()
	defer f.leave()
	match := func(suffix string) bool { return suffix != "" && strings.HasSuffix(file, suffix) }
	switch {
	case match(f.startFails):
		return proc.Result{}, errors.New("cannot start")
	case match(f.timesOut):
		return proc.Result{TimedOut: true}, nil
	case match(f.leftover):
		return proc.Result{Leftover: true}, nil
	case match(f.silentFail):
		return proc.Result{Code: 4}, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		f.w.t.Fatalf("the engine was given an unreadable file: %v", err)
	}
	if strings.Contains(string(data), "SYNTAX_ERROR") {
		return proc.Result{Code: 4, Stderr: "ERROR! bad syntax in " + file + "\n\n"}, nil
	}
	return proc.Result{}, nil
}

// enter and leave count the checks running at once; with a barrier, enter waits for a full set.
func (f *fakeRun) enter() {
	f.mu.Lock()
	f.running++
	f.peak = max(f.peak, f.running)
	barrier := f.barrier
	if f.barrierSlots <= 0 {
		barrier = nil
	} else {
		f.barrierSlots--
	}
	f.mu.Unlock()
	if barrier != nil {
		barrier.Done()
		barrier.Wait()
	}
}

func (f *fakeRun) leave() {
	f.mu.Lock()
	f.running--
	f.mu.Unlock()
}

// checks is the syntax-check commands.
func (f *fakeRun) checks() []proc.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []proc.Cmd
	for _, c := range f.cmds {
		if c.Argv[0] == "ansible-playbook" && slices.Contains(c.Argv, "--syntax-check") {
			out = append(out, c)
		}
	}
	return out
}
