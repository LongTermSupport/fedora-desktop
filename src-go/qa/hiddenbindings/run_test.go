package hiddenbindings

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
	goodShFixture = "#!/usr/bin/env bash\nfzf --bind=q:abort\nfzf --expect=ctrl-n # finding\n"
	goodPyFixture = "from textual.app import App\nclass A(App):\n    BINDINGS = [('d', 'download')]  # finding\n"
	cleanScript   = "#!/usr/bin/env bash\nfzf --bind=q:abort\n"
	keyScript     = "#!/usr/bin/env bash\nfzf --bind=ctrl-a:accept\n"
)

// fakeRunner answers python3 from pyAnswer (given the files handed over) and runs the real
// git, which only lists files.
type fakeRunner struct {
	pyAnswer func(files []string) (proc.Result, error)
	pyCalls  [][]string
}

func (f *fakeRunner) Run(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	if cmd.Argv[0] == "git" {
		return proc.OS{}.Run(ctx, cmd)
	}
	files := cmd.Argv[3:]
	f.pyCalls = append(f.pyCalls, files)
	return f.pyAnswer(files)
}

// helperAnswer reports a finding on line 3 of every handed file whose name contains "keys".
func helperAnswer(t *testing.T) func([]string) (proc.Result, error) {
	t.Helper()
	return func(files []string) (proc.Result, error) {
		type finding struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Message string `json:"message"`
		}
		found := []finding{}
		for _, f := range files {
			if strings.Contains(f, "keys") || strings.HasSuffix(f, "fixture.py") {
				found = append(found, finding{Path: f, Line: 3, Message: "Textual binding d -> download [" + Rule + "]"})
			}
		}
		data, err := json.Marshal(map[string]any{"files": len(files), "findings": found})
		if err != nil {
			t.Fatal(err)
		}
		return proc.Result{Stdout: string(data)}, nil
	}
}

type world struct {
	t      *testing.T
	root   string
	fake   *fakeRunner
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir(), fake: &fakeRunner{pyAnswer: helperAnswer(t)}}
	w.write(shFixture, goodShFixture)
	w.write(pyFixture, goodPyFixture)
	w.write("scripts/qa-trees.tsv", "shell\t.git\npython\t.git\n")
	return w
}

func (w *world) write(rel, text string) {
	w.t.Helper()
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) run(args ...string) ExitCode {
	w.t.Helper()
	env := Env{Stdout: &w.stdout, Stderr: &w.stderr, Cwd: w.root, Runner: w.fake}
	return Main(append([]string{"--root", w.root}, args...), env)
}

func (w *world) gitInit() {
	w.t.Helper()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	for _, argv := range [][]string{{"git", "init", "-q"}, {"git", "add", "-A"}} {
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: argv, Dir: w.root, Env: env, Merge: true})
		if err != nil || res.Code != 0 {
			w.t.Fatalf("%v: %v %d\n%s", argv, err, res.Code, res.Stdout)
		}
	}
}

func TestNamedFilesCleanPass(t *testing.T) {
	w := newWorld(t)
	w.write("bin/tool.bash", cleanScript)
	w.write("bin/app.py", "x = 1\n")
	if code := w.run("bin/tool.bash", "bin/app.py"); code != ExitOK {
		t.Fatalf("exit %d, stderr %s", code, w.stderr.String())
	}
	if got := w.stdout.String(); got != "HIDDEN-BINDINGS-OK 1 shell file(s), 1 Python file(s)\n" {
		t.Errorf("stdout %q", got)
	}
}

func TestFindingsInBothLanguagesFail(t *testing.T) {
	w := newWorld(t)
	w.write("bin/tool.bash", keyScript)
	w.write("bin/keys.py", "x = 1\n")
	if code := w.run("bin/tool.bash", "bin/keys.py"); code != ExitFailed {
		t.Fatalf("exit %d, want 1; stderr %s", code, w.stderr.String())
	}
	out := w.stdout.String()
	for _, want := range []string{"bin/tool.bash:2: fzf --bind ctrl-a:accept", "bin/keys.py:3: Textual binding d -> download"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q lacks %q", out, want)
		}
	}
	if !strings.Contains(w.stderr.String(), "2 action(s) reachable only by a key") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

func TestWholeTreeSkipsTheFixtures(t *testing.T) {
	w := newWorld(t)
	w.write("bin/tool.bash", keyScript)
	w.write("bin/run", "#!/bin/bash\nfzf --expect=ctrl-t\n")
	w.write("lib/app.py", "x = 1\n")
	w.gitInit()
	if code := w.run(); code != ExitFailed {
		t.Fatalf("exit %d, want 1; stderr %s", code, w.stderr.String())
	}
	out := w.stdout.String()
	if strings.Contains(out, fixtureTree) {
		t.Errorf("the fixtures were judged as code: %q", out)
	}
	if !strings.Contains(out, "bin/run:2: fzf --expect") || !strings.Contains(out, "bin/tool.bash:2:") {
		t.Errorf("stdout %q", out)
	}
	last := w.fake.pyCalls[len(w.fake.pyCalls)-1]
	if !slices.Equal(last, []string{"lib/app.py"}) {
		t.Errorf("python handed %v, want [lib/app.py]", last)
	}
}

func TestAShellFixtureTheRuleMissesFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.write(shFixture, "#!/usr/bin/env bash\nfzf --bind=q:abort # finding\n")
	if code := w.run("x.bash"); code != ExitFailed {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(w.stderr.String(), "failed its fixture") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

func TestAPythonFixtureTheRuleMissesFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.write(pyFixture, "x = 1  # finding\n")
	w.fake.pyAnswer = func([]string) (proc.Result, error) {
		return proc.Result{Stdout: `{"files": 1, "findings": []}`}, nil
	}
	if code := w.run("x.bash"); code != ExitFailed || !strings.Contains(w.stderr.String(), "failed its fixture") {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
}

func TestAFixtureWithNoMarkFailsTheRun(t *testing.T) {
	w := newWorld(t)
	w.write(shFixture, cleanScript)
	if code := w.run("x.bash"); code != ExitFailed {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestCannotRun(t *testing.T) {
	cases := map[string]func(w *world) []string{
		"missing fixture": func(w *world) []string {
			if err := os.Remove(filepath.Join(w.root, shFixture)); err != nil {
				t.Fatal(err)
			}
			return []string{"a.bash"}
		},
		"unparseable shell": func(w *world) []string {
			w.write("a.bash", "fzf ( \n")
			return []string{"a.bash"}
		},
		"named file missing": func(*world) []string { return []string{"nope.bash"} },
		"outside the root":   func(*world) []string { return []string{"../x.bash"} },
		"helper exit 2": func(w *world) []string {
			w.fake.pyAnswer = func([]string) (proc.Result, error) { return proc.Result{Code: 2, Stderr: "ERROR: x"}, nil }
			return []string{"a.bash"}
		},
		"helper cannot start": func(w *world) []string {
			w.fake.pyAnswer = func([]string) (proc.Result, error) { return proc.Result{}, errors.New("no python3") }
			return []string{"a.bash"}
		},
		"helper miscounts": func(w *world) []string {
			w.write("a.py", "x = 1\n")
			w.fake.pyAnswer = func(files []string) (proc.Result, error) {
				if len(files) == 1 && strings.HasSuffix(files[0], "fixture.py") {
					return helperAnswer(t)(files)
				}
				return proc.Result{Stdout: `{"files": 0, "findings": []}`}, nil
			}
			return []string{"a.py"}
		},
		"helper prints nonsense": func(w *world) []string {
			w.fake.pyAnswer = func([]string) (proc.Result, error) { return proc.Result{Stdout: "nope"}, nil }
			return []string{"a.bash"}
		},
		"helper times out": func(w *world) []string {
			w.fake.pyAnswer = func([]string) (proc.Result, error) { return proc.Result{TimedOut: true}, nil }
			return []string{"a.bash"}
		},
		"unknown option": func(*world) []string { return []string{"--nope"} },
		"no git tree":    func(*world) []string { return nil },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.write("a.bash", cleanScript)
			if code := w.run(setup(w)...); code != ExitCannotRun {
				t.Fatalf("exit %d, want 2; stderr %s", code, w.stderr.String())
			}
			if !strings.HasPrefix(w.stderr.String(), "ERROR: ") {
				t.Errorf("stderr %q", w.stderr.String())
			}
		})
	}
}

func TestHelpAndFlags(t *testing.T) {
	w := newWorld(t)
	if code := w.run("-h"); code != ExitOK || !strings.Contains(w.stdout.String(), "usage: qa-hidden-bindings") {
		t.Fatalf("exit %d stdout %q", code, w.stdout.String())
	}
	w = newWorld(t)
	w.write("a.bash", cleanScript)
	env := Env{Stdout: &w.stdout, Stderr: &w.stderr, Cwd: t.TempDir(), Runner: w.fake}
	if code := Main([]string{"--scripts", filepath.Join(w.root, "scripts"), "a.bash"}, env); code != ExitOK {
		t.Fatalf("--scripts: exit %d, stderr %s", code, w.stderr.String())
	}
	if code := Main([]string{"--root"}, env); code != ExitCannotRun {
		t.Fatalf("--root with no value: exit %d", code)
	}
}

func TestAnUnwritableStdoutCannotRun(t *testing.T) {
	w := newWorld(t)
	w.write("a.bash", cleanScript)
	env := Env{Stdout: failWriter{}, Stderr: &w.stderr, Cwd: w.root, Runner: w.fake}
	if code := Main([]string{"a.bash"}, env); code != ExitCannotRun {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := Main([]string{"-h"}, env); code != ExitCannotRun {
		t.Fatalf("help: exit %d, want 2", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
