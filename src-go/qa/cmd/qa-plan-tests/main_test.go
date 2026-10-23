package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmaintest"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/plantests"
)

func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }

// A closed stdout cannot carry the verdict, so the command refuses to run.
func TestClosedStdoutIsRefused(t *testing.T) {
	cmdmaintest.RefusesClosedStdout(t, func() int { return run([]string{"--help"}) }, int(plantests.ExitCannotRun))
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(plantests.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(plantests.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// Each token is fresh.
func TestTokensDiffer(t *testing.T) {
	a, errA := token()
	b, errB := token()
	if errA != nil || errB != nil || a == b || len(a) != 32 {
		t.Fatalf("%q %v, %q %v", a, errA, b, errB)
	}
}

// repoFile reads one file of the real repository (the tests run in src-go/qa/cmd/qa-plan-tests).
func repoFile(t *testing.T, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tree is a real git work tree holding the real runner and the plan files given.
type tree struct {
	t    *testing.T
	root string
}

func newTree(t *testing.T) *tree {
	t.Helper()
	tr := &tree{t: t, root: t.TempDir()}
	tr.git("init", "-q")
	tr.write("scripts/qa-trees.tsv", repoFile(t, "scripts", "qa-trees.tsv"))
	for _, name := range []string{"unittest_counts.py", "session_env.py"} {
		tr.write("helpers/qa_environment/"+name, repoFile(t, "helpers", "qa_environment", name))
	}
	tr.git("add", "scripts", "helpers")
	return tr
}

func (tr *tree) git(args ...string) {
	tr.t.Helper()
	argv := append([]string{"git", "-c", "core.hooksPath=/dev/null"}, args...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: argv, Dir: tr.root})
	if err != nil || res.Code != 0 {
		tr.t.Fatalf("git %v: %v %+v", args, err, res)
	}
}

func (tr *tree) write(rel, text string) {
	tr.t.Helper()
	path := filepath.Join(tr.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		tr.t.Fatal(err)
	}
}

func (tr *tree) track(rel, text string) {
	tr.t.Helper()
	tr.write(rel, text)
	tr.git("add", rel)
}

func (tr *tree) run() (int, string, string) {
	tr.t.Helper()
	var stdout, stderr bytes.Buffer
	code := plantests.Main([]string{"--root", tr.root}, newEnv(&stdout, &stderr, tr.root))
	return int(code), stdout.String(), stderr.String()
}

// A plan test imports the checker beside it by name and runs from its own folder, as the plan
// tests do.
const (
	checker = "VALUE = 7\n"
	sibling = "import os\nimport pathlib\nimport unittest\n\nimport checker\n\n\nclass T(unittest.TestCase):\n" +
		"    def test_sibling(self):\n        self.assertEqual(checker.VALUE, 7)\n\n" +
		"    def test_cwd(self):\n        self.assertEqual(pathlib.Path(os.getcwd()).resolve(), pathlib.Path(__file__).resolve().parent)\n"
	failing = "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_one(self):\n        self.assertTrue(False)\n"
)

func TestARealTreeThatPassesIsGreen(t *testing.T) {
	tr := newTree(t)
	tr.track("CLAUDE/Plan/00161-a/checker.py", checker)
	tr.track("CLAUDE/Plan/00161-a/test_checker.py", sibling)
	tr.track("CLAUDE/Plan/00203-b/checker.py", checker)
	tr.track("CLAUDE/Plan/00203-b/test_checker.py", sibling)
	tr.track("CLAUDE/Plan/Completed/00100-old/test_old.py", failing)
	code, stdout, stderr := tr.run()
	if code != 0 || stdout != "PLAN-TESTS-OK 4 test(s) in 2 module(s) in 2 plan folder(s), 0 skipped\n" {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(tr.root, "CLAUDE/Plan/00161-a/__pycache__")); !os.IsNotExist(err) {
		t.Fatalf("the run wrote bytecode into the plan folder: %v", err)
	}
}

func TestARealTreeWithAFailingTestIsExitOneAndNamesTheFolder(t *testing.T) {
	tr := newTree(t)
	tr.track("CLAUDE/Plan/00161-a/test_bad.py", failing)
	tr.track("CLAUDE/Plan/00203-b/checker.py", checker)
	tr.track("CLAUDE/Plan/00203-b/test_checker.py", sibling)
	code, stdout, stderr := tr.run()
	if code != 1 || stdout != "" {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"FAIL: test_one", "ERROR: plan tests failed in CLAUDE/Plan/00161-a", "ERROR: 1 of 2 plan folder(s) failed", "== CLAUDE/Plan/00203-b"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestARealTreeDeletedTestIsNotOwed(t *testing.T) {
	tr := newTree(t)
	tr.track("CLAUDE/Plan/00161-a/checker.py", checker)
	tr.track("CLAUDE/Plan/00161-a/test_checker.py", sibling)
	tr.track("CLAUDE/Plan/00161-a/test_gone.py", failing)
	if err := os.Remove(filepath.Join(tr.root, "CLAUDE/Plan/00161-a/test_gone.py")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := tr.run()
	if code != 0 {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
}

func TestARealTreeWithNoPlanTestsPasses(t *testing.T) {
	tr := newTree(t)
	tr.track("CLAUDE/Plan/00161-a/PLAN.md", "# plan\n")
	code, stdout, stderr := tr.run()
	if code != 0 || stdout != "PLAN-TESTS-OK no test files in the active plan folders\n" {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
}
