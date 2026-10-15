package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	treesText = "# trees\nshell\t.git\nshell\tuntracked\nshell\t.claude/hooks-daemon\nshell\troles/vendor\nshell\tnode_modules\nshell\t.claude/skills\npython\t.venv\n"
	// gatesText is a one-gate table; qaDocText documents it.
	gatesText  = "gate\tg1\tscripts/g1.bash\ttext\tcount\n"
	qaDocText  = "# QA\n\n| Script | What |\n| --- | --- |\n| `g1.bash` | checks |\n"
	claudeText = "# Index\n\n| File | What |\n| --- | --- |\n| [QA.md](CLAUDE/QA.md) | qa |\n"
	mainText   = "---\n- import_playbook: imports/play-a.yml\n"
	playsText  = "# Playbooks\n\nplay-a.yml does a thing.\n"
	archText   = "# Architecture\n\nplay-a.yml is imported.\n"
)

// world is a repository tree on disk plus a fake git that lists what the test says is tracked or
// published. The base tree passes every check; a test changes one thing.
type world struct {
	t        *testing.T
	root     string
	out      string
	run      *fakeRun
	env      map[string]string
	readFail map[string]error
	wrote    []string
}

// newWorld writes the base tree overlaid by files (an empty text removes the path).
func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	all := map[string]string{
		"scripts/qa-trees.tsv":           treesText,
		"scripts/qa-gates.tsv":           gatesText,
		"CLAUDE/QA.md":                   qaDocText,
		"CLAUDE.md":                      claudeText,
		"docs/playbooks.md":              playsText,
		"docs/architecture.md":           archText,
		"playbooks/playbook-main.yml":    mainText,
		"playbooks/imports/play-a.yml":   "- hosts: all\n",
		"scripts/g1.bash":                "#!/usr/bin/bash\n",
		"docs/README.md":                 "# Docs\n",
		".claude/hooks-daemon/README.md": "# vendored\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	w := &world{
		t: t, root: t.TempDir(), out: filepath.Join(t.TempDir(), "report.json"),
		env: map[string]string{}, readFail: map[string]error{},
	}
	w.run = &fakeRun{}
	for rel, text := range all {
		w.add(rel, text)
	}
	return w
}

func (w *world) put(rel, text string) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// add writes a file and makes it tracked (so published).
func (w *world) add(rel, text string) {
	w.t.Helper()
	w.put(rel, text)
	w.run.tracked = append(w.run.tracked, rel)
}

// fresh writes a file that is published but not tracked: new, not ignored.
func (w *world) fresh(rel, text string) {
	w.t.Helper()
	w.put(rel, text)
	w.run.fresh = append(w.run.fresh, rel)
}

// ignored writes a file git neither tracks nor would publish.
func (w *world) ignored(rel, text string) {
	w.t.Helper()
	w.put(rel, text)
}

// doc writes a tracked document under docs/ whose body is the lines.
func (w *world) doc(name string, lines ...string) {
	w.t.Helper()
	w.add("docs/"+name, strings.Join(lines, "\n")+"\n")
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
		ReadFile: func(path string) ([]byte, error) {
			if err, bad := w.readFail[filepath.Base(path)]; bad {
				return nil, err
			}
			return os.ReadFile(filepath.Clean(path))
		},
		Stat:  os.Stat,
		Lstat: os.Lstat,
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

// check runs the gate over the world.
func (w *world) check() result { w.t.Helper(); return w.main() }

// testReport is the machine report as a test reads it.
type testReport struct {
	Type     string         `json:"type"`
	Status   string         `json:"status"`
	Summary  summary        `json:"summary"`
	Vendored vendoredCounts `json:"vendored"`
	Results  []finding      `json:"results"`
	Failures []failure      `json:"failures"`
}

func (w *world) report() testReport {
	w.t.Helper()
	data, err := os.ReadFile(w.out)
	if err != nil {
		w.t.Fatalf("no report: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		w.t.Fatalf("report is not JSON: %v", err)
	}
	for _, key := range []string{"results", "failures"} {
		if string(raw[key]) == "null" || raw[key] == nil {
			w.t.Fatalf("%s is null or absent, not an array", key)
		}
	}
	var rep testReport
	if err := json.Unmarshal(data, &rep); err != nil {
		w.t.Fatal(err)
	}
	return rep
}

func (w *world) noReport() {
	w.t.Helper()
	if _, err := os.Stat(w.out); err == nil {
		w.t.Fatal("a report was written for a run that could not complete")
	}
}

// fakeRun stands in for git.
type fakeRun struct {
	// tracked is what git tracks; fresh is published but untracked; deleted is tracked but gone.
	tracked, fresh, deleted []string
	code                    int
	timesOut, leftover      bool
	startFails              bool
	calls                   [][]string
}

func (f *fakeRun) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	f.calls = append(f.calls, c.Argv)
	if c.Argv[0] != "git" {
		return proc.Result{}, errors.New("unexpected program " + c.Argv[0])
	}
	if _, ok := ctx.Deadline(); !ok {
		return proc.Result{}, errors.New("git was run without a time limit")
	}
	switch {
	case f.startFails:
		return proc.Result{}, errors.New("cannot start")
	case f.timesOut:
		return proc.Result{TimedOut: true}, nil
	case f.leftover:
		return proc.Result{Leftover: true}, nil
	case f.code != 0:
		return proc.Result{Code: f.code, Stderr: "fatal: not a git repository\n"}, nil
	}
	var files []string
	switch flags := strings.Join(c.Argv[2:], " "); flags {
	case "-z":
		files = f.tracked
	case "-z --cached --others --exclude-standard":
		files = append(slices.Clone(f.tracked), f.fresh...)
	case "-z --deleted":
		files = f.deleted
	default:
		return proc.Result{}, errors.New("unexpected flags " + flags)
	}
	var out strings.Builder
	for _, file := range files {
		out.WriteString(file + "\x00")
	}
	return proc.Result{Stdout: out.String()}, nil
}

// wantExit fails unless the run ended with code.
func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, code, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, text string) {
	t.Helper()
	if !strings.Contains(got.stderr, text) {
		t.Fatalf("stderr lacks %q:\n%s", text, got.stderr)
	}
}

func wantStdout(t *testing.T, got result, text string) {
	t.Helper()
	if !strings.Contains(got.stdout, text) {
		t.Fatalf("stdout lacks %q:\n%s", text, got.stdout)
	}
}

// wantFindings runs the gate over the world and requires exactly these finding lines (the part
// of each stdout finding line after the two leading spaces).
func (w *world) wantFindings(want ...string) {
	w.t.Helper()
	got := w.check()
	var lines []string
	for line := range strings.SplitSeq(got.stdout, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "  Details:") {
			lines = append(lines, strings.TrimPrefix(line, "  "))
		}
	}
	if len(want) == 0 {
		wantExit(w.t, got, ExitOK)
		if len(lines) != 0 {
			w.t.Fatalf("clean run printed findings: %q", lines)
		}
		return
	}
	wantExit(w.t, got, ExitFailed)
	if !slices.Equal(lines, want) {
		w.t.Fatalf("findings\n got  %q\n want %q\nstdout:\n%s", lines, want, got.stdout)
	}
}

// wantNamed requires exit 2 with an ERROR line that names the path.
func wantNamed(t *testing.T, got result, path string) {
	t.Helper()
	wantExit(t, got, ExitCannotRun)
	if !strings.HasPrefix(got.stderr, "ERROR: ") || !strings.Contains(got.stderr, path) {
		t.Fatalf("stderr does not name %s:\n%s", path, got.stderr)
	}
	if got.stdout != "" {
		t.Fatalf("stdout %q", got.stdout)
	}
}
