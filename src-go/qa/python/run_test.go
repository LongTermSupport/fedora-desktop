package python

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, code, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, parts ...string) {
	t.Helper()
	wantExit(t, got, ExitCannotRun)
	if !strings.HasPrefix(got.stderr, "ERROR: ") {
		t.Fatalf("stderr does not start with ERROR:\n%s", got.stderr)
	}
	for _, p := range parts {
		if !strings.Contains(got.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, got.stderr)
		}
	}
}

// checkedFiles is the Python files (not templates) a run reported.
func checkedFiles(rep testReport) []string {
	files := make([]string, 0, len(rep.Results))
	for _, r := range rep.Results {
		if r.Type == "python" {
			files = append(files, r.File)
		}
	}
	return files
}

// Y1
func TestHelpPrintsUsageAndExitsZero(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--help")
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "usage: qa-python") || got.stderr != "" || len(w.run.calls) != 0 {
		t.Fatalf("stdout %q stderr %q calls %v", got.stdout, got.stderr, w.run.calls)
	}
}

// Y1
func TestBadArgumentsAreAUsageError(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"--root"}, {"--scripts"}, {"extra"}} {
		w := newWorld(t, nil)
		wantStderr(t, w.main(args...), "usage: qa-python")
		w.noReport()
	}
}

// Y1 (B-Y): an empty or flag-like --root/--scripts value is refused, not read as "not given".
func TestAnEmptyOrFlagLikeDirectoryValueIsRefused(t *testing.T) {
	for _, args := range [][]string{{"--root", ""}, {"--scripts", ""}, {"--root", "--scripts"}} {
		w := newWorld(t, nil)
		got := w.main(args...)
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR: "+args[0]+" needs a directory", "usage: qa-python")
		w.noReport()
	}
}

// Y1: the root is --root, or the parent of --scripts, or the working directory.
func TestRootComesFromTheFlagsOrTheWorkingDirectory(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main("--root", w.root), ExitOK)
	wantExit(t, w.main("--scripts", filepath.Join(w.root, "scripts")), ExitOK)
	wantExit(t, w.main(), ExitOK)
	for _, d := range w.run.dirs {
		if d != w.root {
			t.Fatalf("a command ran in %q, not the root", d)
		}
	}
	wantStderr(t, w.main("--root", t.TempDir()), ".qa-versions")
}

// Y1: the root is made absolute and cleaned once, so '.' and a trailing slash behave as the
// absolute path does (the ruff selection proof compares absolute paths).
func TestARelativeOrTrailingSlashRootIsTheSameRoot(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main("--root", "."), ExitOK)
	wantExit(t, w.main("--root", w.root+"/"), ExitOK)
	wantExit(t, w.main("--scripts", "scripts"), ExitOK)
	wantExit(t, w.main("--scripts", w.root+"/scripts/"), ExitOK)
	for _, d := range w.run.dirs {
		if d != w.root {
			t.Fatalf("a command ran in %q, not the root", d)
		}
	}
	for _, c := range w.run.argv("ruff") {
		for _, a := range c {
			if strings.Contains(a, "//") {
				t.Fatalf("ruff was given an unclean path: %v", c)
			}
		}
	}
}

// Y2
func TestTheReportPathDefaultsAndAnUnwritableOneIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var written string
	var stdout, stderr strings.Builder
	env := Env{
		Getenv: func(string) string { return "" }, Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w.run, TempDir: w.tmp,
		WriteFile: func(path string, _ []byte) error {
			written = path
			return errors.New("disk full")
		},
	}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if written != "/tmp/qa-python-results.json" {
		t.Fatalf("default report path %q", written)
	}
	if !strings.Contains(stderr.String(), "ERROR: cannot write the report /tmp/qa-python-results.json: disk full") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

// Y3
func TestAFailedStdoutWriteIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stderr strings.Builder
	env := Env{
		Getenv: func(string) string { return w.out }, Stdout: failingWriter{}, Stderr: &stderr,
		Cwd: w.root, Runner: w.run, TempDir: w.tmp, WriteFile: func(p string, d []byte) error { return os.WriteFile(p, d, 0o600) },
	}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(stderr.String(), "cannot write to stdout") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// Y4
func TestRuffMustRun(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"not installed":  func(f *fakeRun) { f.ruffMissing = true },
		"exits non-zero": func(f *fakeRun) { f.ruffVersionRC = 1 },
		"odd output":     func(f *fakeRun) { f.ruffVersion = "something else\n" },
	} {
		w := newWorld(t, nil)
		set(w.run)
		got := w.main()
		wantStderr(t, got, "ruff")
		if name == "not installed" {
			wantStderr(t, got, "ruff not installed", "play-python.yml", "dnf")
		}
		w.noReport()
		if len(w.run.argv("python3")) != 0 {
			t.Fatalf("%s: went on to python3", name)
		}
	}
}

// Y4
func TestPython3MustRun(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"not installed":  func(f *fakeRun) { f.pythonMissing = true },
		"exits non-zero": func(f *fakeRun) { f.pythonVersionC = 1 },
	} {
		w := newWorld(t, nil)
		set(w.run)
		wantStderr(t, w.main(), "python3")
		w.noReport()
		if len(w.run.argv("git")) != 0 {
			t.Fatalf("%s: went on to discovery", name)
		}
	}
}

// Y5
func TestThePinMustBeReadable(t *testing.T) {
	for name, text := range map[string]string{
		"missing":   "",
		"no line":   "SEMGREP=1\n",
		"empty":     "RUFF=\n",
		"twice":     "RUFF=0.16.8\nRUFF=0.16.8\n",
		"commented": "# RUFF=0.16.8\n",
	} {
		w := newWorld(t, map[string]string{".qa-versions": text})
		wantStderr(t, w.main(), ".qa-versions")
		w.noReport()
		if len(w.run.argv("git")) != 0 {
			t.Fatalf("%s: went on to discovery", name)
		}
	}
}

// Y5: whitespace around the pin is not part of it.
func TestThePinIgnoresSurroundingWhitespace(t *testing.T) {
	w := newWorld(t, map[string]string{".qa-versions": "RUFF=" + pinnedRuff + "  \r\n"})
	wantExit(t, w.main(), ExitOK)
}

// Y6
func TestAVersionMismatchFailsTheGateAndRunsNoLint(t *testing.T) {
	w := newWorld(t, nil)
	w.run.ruffVersion = "ruff 0.17.0\n"
	got := w.main()
	wantExit(t, got, ExitFailed)
	want := "✗ python: ruff version mismatch: expected 0.16.8 (.qa-versions), found 0.17.0 → " + w.out + " (no files linted)\n"
	if got.stdout != want {
		t.Fatalf("stdout %q\nwant %q", got.stdout, want)
	}
	if !strings.Contains(got.stderr, "expected : 0.16.8") || !strings.Contains(got.stderr, "found    : 0.17.0") || strings.Contains(got.stderr, "ERROR:") {
		t.Fatalf("stderr %q", got.stderr)
	}
	rep := w.report()
	if rep.Status != "fail" || rep.Type != "python" || rep.Summary != (summary{Total: 0, Passed: 0, Failed: 1}) {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Results) != 0 || len(rep.RuffDiagnostics) != 0 {
		t.Fatalf("report %+v", rep)
	}
	wantFailure := entry{
		File: ".qa-versions", Type: "python", Status: "fail",
		Error: "ruff version mismatch: expected 0.16.8, found 0.17.0 — ruff was not run, so no python file was linted this run",
	}
	if len(rep.Failures) != 1 || rep.Failures[0] != wantFailure {
		t.Fatalf("failures %+v", rep.Failures)
	}
}

// Y6: ruff is asked its version and nothing else, and nothing is discovered.
func TestAVersionMismatchAsksRuffNothingElse(t *testing.T) {
	w := newWorld(t, nil)
	w.run.ruffVersion = "ruff 0.17.0\n"
	wantExit(t, w.main(), ExitFailed)
	if len(w.run.argv("git")) != 0 {
		t.Fatalf("discovery ran: %v", w.run.calls)
	}
	for _, c := range w.run.argv("ruff") {
		if !slices.Equal(c, []string{"ruff", "--version"}) {
			t.Fatalf("ruff was asked to do more: %v", c)
		}
	}
}

// Y7
func TestGitFailingIsExitTwoNamingTheRoot(t *testing.T) {
	w := newWorld(t, nil)
	w.run.gitFails = 128
	wantStderr(t, w.main(), "cannot list the files git would publish under "+w.root)
	w.noReport()
}

// Y7, Y8, Y9: the files are those git lists, outside the python trees, classified by what they are.
func TestOnlyPythonFilesOutsideTheExcludedTreesAreChecked(t *testing.T) {
	w := newWorld(t, map[string]string{
		"a/mod.py":             "x = 1\n",
		"bin/tool":             "#!/usr/bin/env python3\nprint()\n",
		"bin/old":              "#!/usr/bin/python\nprint()\n",
		"bin/tool.sh":          "#!/usr/bin/env python3\n",
		"bin/shell":            "#!/usr/bin/bash\necho python\n",
		"bin/empty":            "",
		"notes.txt":            "python\n",
		".venv/lib/x.py":       "x = 1\n",
		"untracked/scratch.py": "x = 1\n",
		"tpl/render.py.j2":     "#!/usr/bin/env python3\n",
		"deep/.venv/y.py":      "x = 1\n",
		"deep/not.venv/z.py":   "x = 1\n",
	})
	wantExit(t, w.main(), ExitOK)
	// bin/tool.sh has a python shebang and a .sh name: it is Python by what it is.
	want := []string{"a/mod.py", "bin/old", "bin/tool", "bin/tool.sh", "deep/not.venv/z.py", "tool.py"}
	if got := checkedFiles(w.report()); !slices.Equal(got, want) {
		t.Fatalf("checked %v\nwant    %v", got, want)
	}
}

// Y9: the mode plays no part.
func TestTheExecuteBitPlaysNoPart(t *testing.T) {
	w := newWorld(t, map[string]string{"bin/plain": "#!/usr/bin/env python3\n"})
	wantExit(t, w.main(), ExitOK)
	if !slices.Contains(checkedFiles(w.report()), "bin/plain") {
		t.Fatalf("bin/plain was not checked: %+v", w.report())
	}
}

// Y9: a symlink is checked under its own name.
func TestASymlinkIsCheckedUnderItsOwnName(t *testing.T) {
	w := newWorld(t, nil)
	w.link("alias.py", "tool.py")
	wantExit(t, w.main(), ExitOK)
	if got := checkedFiles(w.report()); !slices.Equal(got, []string{"alias.py", "tool.py"}) {
		t.Fatalf("checked %v", got)
	}
}

// Y9: a symlink named .py that cannot be followed is exit 2 naming it; any other that cannot be
// followed leads to nothing and is no program of ours (a link into a directory git ignores).
func TestADanglingSymlink(t *testing.T) {
	w := newWorld(t, nil)
	w.link("dangling.py", "nowhere.py")
	wantStderr(t, w.main(), "dangling.py", "cannot be followed")
	w.noReport()

	w = newWorld(t, nil)
	w.link("vault", "../vendor/nowhere/")
	wantExit(t, w.main(), ExitOK)
}

// Y9: a path git lists that the tree lacks and --deleted does not name.
func TestAListedPathTheTreeLacksIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.files = append(w.run.files, "ghost.py")
	wantStderr(t, w.main(), "cannot inspect ghost.py")
}

// Y9: a candidate that cannot be read is exit 2 naming it.
func TestAnUnreadableCandidateIsExitTwo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	w := newWorld(t, map[string]string{"bin/locked": "#!/usr/bin/env python3\n"})
	if err := os.Chmod(filepath.Join(w.root, "bin/locked"), 0); err != nil {
		t.Fatal(err)
	}
	wantStderr(t, w.main(), "bin/locked")
}

// Y9: a directory (or submodule) entry is not a file.
func TestADirectoryEntryIsNotAFile(t *testing.T) {
	w := newWorld(t, nil)
	if err := os.MkdirAll(filepath.Join(w.root, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.run.files = append(w.run.files, "sub")
	wantExit(t, w.main(), ExitOK)
}

// Y10
func TestNoPythonFileIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"tool.py": ""})
	wantStderr(t, w.main(), "found 0 Python files")
	w.noReport()
}

// Y8: a table with no python row.
func TestTheTreesTableMustHaveAPythonRow(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/qa-trees.tsv": "python\t.venv\n"})
	wantStderr(t, w.main(), "'shell' row and a 'python' row")
	w.noReport()
}
