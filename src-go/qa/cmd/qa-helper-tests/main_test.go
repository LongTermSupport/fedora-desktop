package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/helpertests"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(helpertests.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(helpertests.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// repoFile reads one file of the real repository (the tests run in src-go/qa/cmd/qa-helper-tests).
func repoFile(t *testing.T, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tree is a real git work tree holding the real runner, the real trees table and the tests given;
// the files named in tracked are in git's index.
type tree struct {
	t    *testing.T
	root string
}

func newTree(t *testing.T) *tree {
	t.Helper()
	tr := &tree{t: t, root: t.TempDir()}
	tr.git("init", "-q")
	tr.write("scripts/qa-trees.tsv", repoFile(t, "scripts", "qa-trees.tsv"))
	// The runner and the module it imports (the session scrub every helper test run gets)
	for _, name := range []string{"unittest_counts.py", "session_env.py"} {
		tr.write("helpers/qa_environment/"+name, repoFile(t, "helpers", "qa_environment", name))
	}
	tr.git("add", "scripts", "helpers")
	return tr
}

func (tr *tree) git(args ...string) {
	tr.t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: tr.root})
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

// track writes a test and adds it to the index.
func (tr *tree) track(rel, text string) {
	tr.t.Helper()
	tr.write(rel, text)
	tr.git("add", rel)
}

func (tr *tree) run(args ...string) (int, string, string) {
	tr.t.Helper()
	var stdout, stderr bytes.Buffer
	env := newEnv(&stdout, &stderr, tr.root)
	code := helpertests.Main(append([]string{"--root", tr.root}, args...), env)
	return int(code), stdout.String(), stderr.String()
}

const (
	passing = "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_one(self):\n        self.assertTrue(True)\n\n    @unittest.skip('x')\n    def test_skipped(self):\n        pass\n"
	failing = "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_one(self):\n        self.assertTrue(False)\n"
)

// H1-H13 over a real git index, the real discovery and the real runner.
func TestARealTreeThatPassesIsGreenAndWritesCountsTheReaderTrusts(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	tr.track("tests/helpers/sub/test_b.py", passing)
	counts := filepath.Join(t.TempDir(), "counts")
	code, stdout, stderr := tr.run("--counts-file", counts, "--counts-token", "nonce-1")
	if code != 0 || stdout != "" {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"COVERAGE: 2 of 2 tracked helper test modules", "Ran 4 tests", "HELPER-TESTS-OK: Ran 4 tests in 2 modules (2 tracked), 2 skipped"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	// The contract with qa-all: its reader accepts what the real runner wrote through this gate.
	summary, err := gates.HelperCounts(counts, "nonce-1")
	if err != nil || summary != "Ran 4 tests in 2 modules (2 tracked), 2 skipped" {
		t.Fatalf("qa-all would read %q, %v", summary, err)
	}
}

func TestARealTreeWithAFailingTestIsExitOne(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	tr.track("tests/helpers/test_bad.py", failing)
	code, stdout, stderr := tr.run()
	if code != 1 || stdout != "" {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"FAIL: test_one", "AssertionError", "ERROR: helper tests failed (3 run, 1 skipped)"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestARealTreeUntrackedTestRunsAndIsNamedAndADeletedOneIsNotRun(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	tr.track("tests/helpers/test_gone.py", passing)
	if err := os.Remove(filepath.Join(tr.root, "tests/helpers/test_gone.py")); err != nil {
		t.Fatal(err)
	}
	tr.write("tests/helpers/test_new.py", passing)
	code, _, stderr := tr.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	for _, want := range []string{"COVERAGE: 1 of 1 tracked", "plus 1 UNTRACKED file(s)", "tests/helpers/test_new.py", "(1 tracked)", "in 2 modules"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestARealTreeWithNoTestsIsExitTwo(t *testing.T) {
	tr := newTree(t)
	tr.write("tests/helpers/notes.txt", "x")
	code, stdout, stderr := tr.run()
	if code != 2 || stdout != "" || !strings.Contains(stderr, "ERROR: ") {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
}

// A test that cannot be imported is a failing test of the runner's own, not a skip: the runner
// reports an import error as an error and exits 1.
func TestARealTreeTestThatCannotBeImportedIsAFailureNotASkip(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	tr.track("tests/helpers/test_broken.py", "import does_not_exist_anywhere\n")
	code, _, stderr := tr.run()
	if code != 1 || !strings.Contains(stderr, "ModuleNotFoundError") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}

// A runner that is not there is exit 2, never a pass.
func TestARealTreeWithoutTheRunnerIsExitTwo(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	if err := os.Remove(filepath.Join(tr.root, "helpers/qa_environment/unittest_counts.py")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := tr.run()
	if code != 2 || stdout != "" || !strings.Contains(stderr, "the runner ended") {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout, stderr)
	}
}

// The git configuration of the machine stays out of the suite (H8): a test sees the isolation.
func TestARealTreeRunsTheSuiteWithTheMachinesGitConfigIsolated(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_env.py", "import os\nimport unittest\n\n\nclass T(unittest.TestCase):\n"+
		"    def test_isolated(self):\n"+
		"        self.assertEqual(os.environ.get('GIT_CONFIG_GLOBAL'), '/dev/null')\n"+
		"        self.assertEqual(os.environ.get('GIT_CONFIG_NOSYSTEM'), '1')\n"+
		"        self.assertNotIn('GIT_CONFIG_COUNT', os.environ)\n"+
		"        self.assertNotIn('GIT_CONFIG_PARAMETERS', os.environ)\n")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'a.b=c'")
	code, _, stderr := tr.run()
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}

// The root given through a symlink is resolved, and no checkout path leaks into stdout.
func TestARealTreeThroughASymlinkedRootStillRuns(t *testing.T) {
	tr := newTree(t)
	tr.track("tests/helpers/test_a.py", passing)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(tr.root, link); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := newEnv(&stdout, &stderr, link)
	if code := helpertests.Main([]string{"--root", link}, env); code != helpertests.ExitOK || stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q:\n%s", code, stdout.String(), stderr.String())
	}
}
