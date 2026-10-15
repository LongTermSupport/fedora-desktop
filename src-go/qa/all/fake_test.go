package all

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// reply is what the fake runner answers for one gate: its streams, status, and the files a
// stage or the helper suite writes beside its output.
type reply struct {
	stdout, stderr string
	code           int
	timedOut       bool
	leftover       bool
	err            error
	// report is a stage's JSON report, written to QA_JSON_OUT; empty writes a clean one.
	report string
	// counts is the helper suite's counts file; %TOKEN% is the token it was handed. Empty
	// writes a well-formed one.
	counts string
	// onRun is called when the gate runs, before it answers (an interrupt, for one).
	onRun func()
}

const (
	cleanReport = `{"type":"x","status":"pass","summary":{"total":4,"passed":4,"failed":0},"failures":[]}`
	cleanCounts = "token=%TOKEN%\ntests=10\nskipped=1\nmodules=2\ntracked=2\n"
)

// fakeRunner stands in for every gate script: it logs which gate ran, in order, and answers
// as the test says. A gate with no reply passes with a case count; a stage or the helper suite,
// whose results travel in files, passes with no output at all.
type fakeRunner struct {
	t       *testing.T
	replies map[string]reply
	log     []string
	dirs    []string
}

func newFakeRunner(t *testing.T) *fakeRunner {
	t.Helper()
	return &fakeRunner{t: t, replies: map[string]reply{}}
}

// keyOf names a command: a bash script by its file name, anything else by its words, an
// absolute path (the repository root a {root} word became) by its last element, so the key does
// not depend on which temporary directory the world was built in.
func keyOf(argv []string) string {
	if argv[0] == "bash" {
		return filepath.Base(argv[1])
	}
	words := make([]string, len(argv))
	for i, word := range argv {
		words[i] = word
		if filepath.IsAbs(word) {
			words[i] = filepath.Base(word)
		}
	}
	return strings.Join(words, " ")
}

func envOf(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func argAfter(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// Run implements proc.Runner.
func (f *fakeRunner) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := keyOf(c.Argv)
	f.log = append(f.log, key)
	f.dirs = append(f.dirs, c.Dir)
	stage, helper := envOf(c.Env, "QA_JSON_OUT"), argAfter(c.Argv, "--counts-file")
	r, ok := f.replies[key]
	if !ok && stage == "" && helper == "" {
		r = reply{stdout: "passed: 1 failed: 0\n"}
	}
	if r.onRun != nil {
		r.onRun()
	}
	if r.err != nil {
		return proc.Result{}, r.err
	}
	if stage != "" {
		f.write(stage, orDefault(r.report, cleanReport))
	}
	if helper != "" {
		f.write(helper, strings.ReplaceAll(orDefault(r.counts, cleanCounts), "%TOKEN%", argAfter(c.Argv, "--counts-token")))
	}
	res := proc.Result{Code: r.code, TimedOut: r.timedOut, Leftover: r.leftover}
	f.deliver(&res, r, c)
	return res, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (f *fakeRunner) write(path, body string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// deliver puts the reply's streams where the command asked for them: to its writers, or into
// the result (merged, or split).
func (f *fakeRunner) deliver(res *proc.Result, r reply, c proc.Cmd) {
	f.t.Helper()
	if c.Merge {
		res.Stdout = r.stdout + r.stderr
		return
	}
	res.Stdout = f.stream(c.StdoutTo, r.stdout)
	res.Stderr = f.stream(c.StderrTo, r.stderr)
}

// stream writes text to the command's writer when it has one, else returns it for the result.
func (f *fakeRunner) stream(to io.Writer, text string) string {
	f.t.Helper()
	if to == nil {
		return text
	}
	if _, err := io.WriteString(to, text); err != nil {
		f.t.Fatal(err)
	}
	return ""
}

// world is a scratch repository root holding scripts/qa-gates.tsv and an empty file for every
// script it names, plus the streams and the fake runner a run writes to.
type world struct {
	t      *testing.T
	root   string
	runner *fakeRunner
	out    strings.Builder
	errOut strings.Builder
	env    map[string]string
}

func newWorld(t *testing.T, table string) *world {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o750); err != nil {
		t.Fatal(err)
	}
	tablePath := filepath.Join(scripts, "qa-gates.tsv")
	if err := os.WriteFile(tablePath, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	gs, err := gates.Load(tablePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gs {
		if name, ok := g.Script(); ok {
			if err := os.WriteFile(filepath.Join(scripts, name), []byte("#!/usr/bin/bash\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &world{t: t, root: root, runner: newFakeRunner(t), env: map[string]string{"QA_RESULTS_JSON": filepath.Join(root, "results.json")}}
}

func (w *world) main(ctx context.Context, args ...string) ExitCode {
	w.t.Helper()
	w.out.Reset()
	w.errOut.Reset()
	env := Env{
		Getenv:   func(k string) string { return w.env[k] },
		Stdout:   &w.out,
		Stderr:   &w.errOut,
		Runner:   w.runner,
		Cwd:      w.root,
		NewToken: func() string { return "tok-1" },
	}
	return Main(ctx, append([]string{"--root", w.root}, args...), env)
}

func (w *world) results() string {
	w.t.Helper()
	body, err := os.ReadFile(w.env["QA_RESULTS_JSON"])
	if err != nil {
		w.t.Fatal(err)
	}
	return string(body)
}

func stdoutLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
