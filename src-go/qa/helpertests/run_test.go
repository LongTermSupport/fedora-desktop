package helpertests

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// H1: the command line.
func TestH1HelpGoesToStdoutAndExitsZero(t *testing.T) {
	w := goodWorld(t)
	for _, flag := range []string{"-h", "--help"} {
		w.out.Reset()
		if code := w.run(flag); code != ExitOK {
			t.Fatalf("%s: exit %d", flag, code)
		}
		mustContain(t, "stdout", w.out.String(), "usage: qa-helper-tests", "--counts-file", "--counts-token")
	}
	if len(w.ran) != 0 {
		t.Fatal("help ran something")
	}
}

func TestH1ABadCommandLineIsExitTwoWithTheUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown option", []string{"--nope"}, "unknown argument '--nope'"},
		{"a positional", []string{"tests/helpers/test_a.py"}, "unknown argument 'tests/helpers/test_a.py'"},
		{"counts file with no value", []string{"--counts-file"}, "--counts-file needs a value"},
		{"counts file with an empty value", []string{"--counts-file", ""}, "--counts-file needs a value"},
		{"counts file followed by an option", []string{"--counts-file", "--counts-token", "x"}, "--counts-file needs a value"},
		{"token with no value", []string{"--counts-token"}, "--counts-token needs a value"},
		{"root with no value", []string{"--root"}, "--root needs a value"},
		{"scripts with no value", []string{"--scripts"}, "--scripts needs a value"},
		{"a repeated counts file", []string{"--counts-file", "/a", "--counts-file", "/b"}, "--counts-file given twice"},
		{"a repeated token", []string{"--counts-token", "a", "--counts-token", "b"}, "--counts-token given twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := goodWorld(t)
			if code := w.run(c.args...); code != ExitCannotRun {
				t.Fatalf("exit %d", code)
			}
			mustContain(t, "stderr", w.err.String(), "ERROR: "+c.want)
			if w.out.String() != "" || len(w.ran) != 0 {
				t.Fatalf("stdout %q, ran %d", w.out.String(), len(w.ran))
			}
			if strings.Contains(c.want, "unknown") {
				mustContain(t, "stderr", w.err.String(), "usage: qa-helper-tests")
			}
		})
	}
}

func TestH1TheRootIsTheGivenOneTheParentOfScriptsOrTheWorkingDirectory(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--root", "/given"}, "/given"},
		{[]string{"--scripts", "/given/scripts"}, "/given"},
		{nil, "/repo"},
	}
	for _, c := range cases {
		w := goodWorld(t)
		if code := w.runWith(c.args...); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", c.args, code, w.err.String())
		}
		if w.ran[0].Dir != c.want {
			t.Fatalf("%v: ran in %q, want %q", c.args, w.ran[0].Dir, c.want)
		}
	}
}

func TestH1AnUnresolvableRootIsExitTwo(t *testing.T) {
	w := goodWorld(t)
	env := w.env()
	env.Resolve = func(string) (string, error) { return "", errors.New("no such directory") }
	if code := Main([]string{"--root", "/gone"}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: cannot resolve the repository root")
}

// H2, H3: exit codes and streams.
func TestH2AGreenRunIsExitZeroWithNothingOnStdout(t *testing.T) {
	w := goodWorld(t)
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "" {
		t.Fatalf("stdout %q", w.out.String())
	}
}

func TestH2ARedSuiteIsExitOneWithNothingOnStdout(t *testing.T) {
	w := goodWorld(t)
	w.code = 1
	if code := w.runWith(); code != ExitFailed {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "" {
		t.Fatalf("stdout %q", w.out.String())
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: helper tests failed (7 run, 1 skipped)")
}

func TestH3AFailedWriteIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*Env){
		"stderr": func(e *Env) { e.Stderr = failWriter{} },
		"stdout": func(e *Env) { e.Stdout = failWriter{} },
	} {
		t.Run(name, func(t *testing.T) {
			w := goodWorld(t)
			w.answer = func(w *world, c proc.Cmd) (proc.Result, error) {
				if _, err := c.StdoutTo.Write([]byte("a test printed this\n")); err != nil {
					t.Log(err)
				}
				return w.defaultAnswer(c)
			}
			env := w.env()
			set(&env)
			counts := filepath.Join(t.TempDir(), "c")
			if code := Main([]string{"--counts-file", counts}, env); code != ExitCannotRun {
				t.Fatalf("exit %d", code)
			}
		})
	}
}

func TestH3AFailedStdoutWriteOfHelpSaysSo(t *testing.T) {
	w := goodWorld(t)
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main([]string{"--help"}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: cannot write to stdout: closed pipe")
}

// H4: what is found.
func TestH4TheTestsAreFoundByNameAtAnyDepth(t *testing.T) {
	w := goodWorld(t)
	w.add("tests/helpers/a/b/c/test_deep.py", "x")
	w.add("tests/helpers/a/helper_not_a_test.py", "x")
	w.add("tests/helpers/a/test_notpy.txt", "x")
	w.add("tests/helpers/a/test_x.pyc", "x")
	w.add("tests/helpers/a/atest_x.py", "x")
	w.add("tests/helpers/a/__init__.py", "")
	w.tracked = append(w.tracked, "tests/helpers/a/b/c/test_deep.py")
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	got := modulesOf(w.ran[0].Argv)
	want := []string{"tests.helpers.a.b.c.test_deep", "tests.helpers.pkg.test_b", "tests.helpers.test_a"}
	if !slices.Equal(got, want) {
		t.Fatalf("modules %v, want %v", got, want)
	}
}

func TestH4ADirectoryNamedLikeATestIsNotATest(t *testing.T) {
	w := goodWorld(t)
	w.add("tests/helpers/test_dir.py/inner.py", "x")
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	mustNotContain(t, "argv", strings.Join(w.ran[0].Argv, " "), "test_dir")
}

func TestH4NoTestAtAllIsExitTwo(t *testing.T) {
	w := &world{t: t, files: fstest.MapFS{"tests/helpers/readme.txt": &fstest.MapFile{Data: []byte("x")}}}
	if code := w.runWith(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: found 0 helper tests under tests/helpers")
	if len(w.ran) != 0 {
		t.Fatal("ran with no scope")
	}
}

func TestH4AMissingTestsDirectoryIsExitTwo(t *testing.T) {
	w := &world{t: t, files: fstest.MapFS{}}
	if code := w.runWith(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: cannot read tests/helpers")
}

// unreadableFS fails to list one directory.
type unreadableFS struct {
	fstest.MapFS

	bad string
}

func (u unreadableFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == u.bad {
		return nil, errors.New("permission denied")
	}
	return u.MapFS.ReadDir(name)
}

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.bad {
		return nil, errors.New("permission denied")
	}
	return u.MapFS.Open(name)
}

func TestH4AnUnreadableDirectoryIsExitTwoNamingIt(t *testing.T) {
	w := goodWorld(t)
	w.add("tests/helpers/locked/test_c.py", "x")
	env := w.env()
	env.Open = func(string) fs.FS { return unreadableFS{MapFS: w.files, bad: "tests/helpers/locked"} }
	if code := Main([]string{"--counts-file", filepath.Join(t.TempDir(), "c")}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: cannot read tests/helpers/locked", "permission denied")
	if len(w.ran) != 0 {
		t.Fatal("ran past an unreadable directory")
	}
}

func TestH4ASymlinkedTestIsFollowedAndADanglingOneIsExitTwo(t *testing.T) {
	w := goodWorld(t)
	w.files["tests/helpers/test_link.py"] = &fstest.MapFile{Data: []byte("test_a.py"), Mode: fs.ModeSymlink}
	w.tracked = append(w.tracked, "tests/helpers/test_link.py")
	slices.Sort(w.tracked)
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if !slices.Contains(modulesOf(w.ran[0].Argv), "tests.helpers.test_link") {
		t.Fatalf("the link was not collected: %v", modulesOf(w.ran[0].Argv))
	}
}

// resolveFS is a file system whose Stat of a named path fails the way a dangling link does.
type resolveFS struct {
	fstest.MapFS

	dangling string
}

func (r resolveFS) Stat(name string) (fs.FileInfo, error) {
	if name == r.dangling {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return r.MapFS.Stat(name)
}

func TestH4ADanglingTestLinkIsExitTwoNamingIt(t *testing.T) {
	w := goodWorld(t)
	w.files["tests/helpers/test_dangling.py"] = &fstest.MapFile{Data: []byte("nowhere.py"), Mode: fs.ModeSymlink}
	env := w.env()
	env.Open = func(string) fs.FS { return resolveFS{MapFS: w.files, dangling: "tests/helpers/test_dangling.py"} }
	if code := Main([]string{"--counts-file", filepath.Join(t.TempDir(), "c")}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: the symlink tests/helpers/test_dangling.py cannot be followed")
}

func TestH4ALinkedDirectoryIsNotEntered(t *testing.T) {
	w := goodWorld(t)
	w.files["tests/helpers/linked"] = &fstest.MapFile{Data: []byte("pkg"), Mode: fs.ModeSymlink}
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	mustNotContain(t, "argv", strings.Join(w.ran[0].Argv, " "), "linked")
}

// H5: the coverage cross-check.
func TestH5ATrackedTestTheWalkMissedIsExitTwoNamingEachBeforeAnythingRuns(t *testing.T) {
	w := goodWorld(t)
	w.tracked = append(w.tracked, "tests/helpers/ghost/test_one.py", "tests/helpers/ghost/test_two.py")
	if code := w.runWith(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(),
		"ERROR: discovery missed 2 tracked helper test file(s):",
		"    tests/helpers/ghost/test_one.py", "    tests/helpers/ghost/test_two.py",
		"Fix the discovery")
	if len(w.ran) != 0 {
		t.Fatal("ran with lost tests")
	}
}

func TestH5ADiscoveryFailureIsExitTwoCarryingItsMessage(t *testing.T) {
	w := goodWorld(t)
	w.trackedErr = errors.New("git exited 128: not a repository")
	if code := w.runWith(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: ", "git exited 128: not a repository")
	if len(w.ran) != 0 {
		t.Fatal("ran without a yardstick")
	}
}

// H6: the other direction.
func TestH6AnUntrackedTestStillRunsAndIsNamed(t *testing.T) {
	w := goodWorld(t)
	w.add("tests/helpers/test_new.py", "x")
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	mustContain(t, "stderr", w.err.String(),
		"COVERAGE: 2 of 2 tracked helper test modules",
		"plus 1 UNTRACKED file(s)", "    tests/helpers/test_new.py", "incomparable")
	if !slices.Contains(modulesOf(w.ran[0].Argv), "tests.helpers.test_new") {
		t.Fatal("the untracked test did not run")
	}
	if argAfter(w.ran[0].Argv, "--tracked-modules") != "2" {
		t.Fatalf("tracked count %q", argAfter(w.ran[0].Argv, "--tracked-modules"))
	}
}

func TestH6ACleanTreeReportsFullCoverageAndNoUntracked(t *testing.T) {
	w := goodWorld(t)
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "COVERAGE: 2 of 2 tracked helper test modules")
	mustNotContain(t, "stderr", w.err.String(), "UNTRACKED")
}

// H7: module names.
func TestH7APathThatIsNotADottedIdentifierIsExitTwoNamingIt(t *testing.T) {
	for _, bad := range []string{
		"tests/helpers/test_with-dash.py",
		"tests/helpers/test_a.b.py",
		"tests/helpers/with space/test_x.py",
		"tests/helpers/1st/test_x.py",
		"tests/helpers/ünï/test_x.py",
	} {
		t.Run(bad, func(t *testing.T) {
			w := goodWorld(t)
			w.add(bad, "x")
			w.tracked = append(w.tracked, bad)
			if code := w.runWith(); code != ExitCannotRun {
				t.Fatalf("exit %d", code)
			}
			mustContain(t, "stderr", w.err.String(), "ERROR: "+bad+" cannot be imported by name")
			if len(w.ran) != 0 {
				t.Fatal("ran with a name it could not import")
			}
		})
	}
}

// H8: the invocation.
func TestH8TheRunnerIsStartedExactlyAsSpecified(t *testing.T) {
	w := goodWorld(t)
	counts := filepath.Join(t.TempDir(), "counts")
	if code := w.run("--root", "/the/root", "--counts-file", counts, "--counts-token", testToken); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := []string{
		"env", "-u", "GIT_CONFIG_COUNT", "-u", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"python3", "-m", "helpers.qa_environment.unittest_counts",
		"--counts-file", counts, "--tracked-modules", "2", "--counts-token", testToken,
		"tests.helpers.pkg.test_b", "tests.helpers.test_a",
	}
	if len(w.ran) != 1 || !slices.Equal(w.ran[0].Argv, want) {
		t.Fatalf("argv\n got %v\nwant %v", w.ran[0].Argv, want)
	}
	if w.ran[0].Dir != "/the/root" {
		t.Fatalf("dir %q", w.ran[0].Dir)
	}
	if w.ran[0].Merge {
		t.Fatal("the streams must stay apart")
	}
}

// H9: the counts file.
func TestH9ATokenIsForwardedOnlyWhenGiven(t *testing.T) {
	w := goodWorld(t)
	if code := w.run("--counts-file", filepath.Join(t.TempDir(), "c")); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if slices.Contains(w.ran[0].Argv, "--counts-token") {
		t.Fatalf("a token nobody gave: %v", w.ran[0].Argv)
	}
}

func TestH9ARelativeCountsFileIsMadeAbsoluteAgainstTheCallersDirectory(t *testing.T) {
	w := goodWorld(t)
	dir := t.TempDir()
	env := w.env()
	env.Cwd = dir
	if code := Main([]string{"--root", "/the/root", "--counts-file", "sub/counts"}, env); code != ExitCannotRun {
		// sub/ does not exist, so the file cannot be created: still the path must have been absolute.
		t.Logf("exit %d", code)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.err.Reset()
	if code := Main([]string{"--root", "/the/root", "--counts-file", "sub/counts"}, env); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if got := argAfter(w.ran[len(w.ran)-1].Argv, "--counts-file"); got != filepath.Join(dir, "sub", "counts") {
		t.Fatalf("counts file %q", got)
	}
}

func TestH9TheCountsFileIsEmptiedBeforeTheRunNeverTrustingAStaleOne(t *testing.T) {
	w := goodWorld(t)
	counts := filepath.Join(t.TempDir(), "counts")
	if err := os.WriteFile(counts, []byte("tests=99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("--counts-file", counts); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.countsSeen != "" {
		t.Fatalf("the runner started with %q in the counts file", w.countsSeen)
	}
}

func TestH9AStaleCountsFileFromARunnerThatWroteNothingIsExitTwo(t *testing.T) {
	w := goodWorld(t)
	counts := filepath.Join(t.TempDir(), "counts")
	if err := os.WriteFile(counts, []byte("tests=7\nskipped=0\nmodules=2\ntracked=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.answer = func(*world, proc.Cmd) (proc.Result, error) { return proc.Result{Code: 0}, nil }
	if code := w.run("--counts-file", counts); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: the runner ended 0 but its counts could not be read", "is empty")
}

func TestH9WithNoCountsFileAPrivateOneIsUsedAndRemoved(t *testing.T) {
	w := goodWorld(t)
	if code := w.run(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.countsPath == "" || !filepath.IsAbs(w.countsPath) {
		t.Fatalf("counts path %q", w.countsPath)
	}
	if _, err := os.Stat(w.countsPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the private counts file was left behind: %v", err)
	}
}

func TestH9ACountsFileThatCannotBeEmptiedIsExitTwo(t *testing.T) {
	w := goodWorld(t)
	if code := w.run("--counts-file", filepath.Join(t.TempDir(), "absent-dir", "counts")); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: cannot prepare the counts file")
	if len(w.ran) != 0 {
		t.Fatal("ran without a place for its counts")
	}
}

// H10: the runner's streams are passed through.
func TestH10TheRunnersStreamsReachTheCallersStreamsLive(t *testing.T) {
	w := goodWorld(t)
	w.answer = func(w *world, c proc.Cmd) (proc.Result, error) {
		if _, err := c.StdoutTo.Write([]byte("from the runner's stdout\n")); err != nil {
			t.Error(err)
		}
		if _, err := c.StderrTo.Write([]byte("from the runner's stderr\n")); err != nil {
			t.Error(err)
		}
		return w.defaultAnswer(c)
	}
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if w.out.String() != "from the runner's stdout\n" {
		t.Fatalf("stdout %q", w.out.String())
	}
	mustContain(t, "stderr", w.err.String(), "from the runner's stderr")
}

// H11: the runner's status.
func TestH11OnlyZeroAndOneAreVerdicts(t *testing.T) {
	cases := []struct {
		name string
		res  proc.Result
		err  error
		want ExitCode
		msg  string
	}{
		{"killed by a signal", proc.Result{Code: -1}, nil, ExitCannotRun, "ended -1"},
		{"python3 or env missing", proc.Result{Code: 127}, nil, ExitCannotRun, "ended 127"},
		{"an argument error", proc.Result{Code: 2}, nil, ExitCannotRun, "ended 2"},
		{"cannot be started", proc.Result{}, errors.New("exec: not found"), ExitCannotRun, "play-python.yml"},
		{"timed out", proc.Result{TimedOut: true, Code: -1}, nil, ExitCannotRun, "timed out"},
		{"a leftover process", proc.Result{Leftover: true}, nil, ExitCannotRun, "left a process"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := goodWorld(t)
			// The runner leaves agreeing counts behind, so only the status can fail the run.
			w.answer = func(w *world, cmd proc.Cmd) (proc.Result, error) {
				if _, err := w.defaultAnswer(cmd); err != nil {
					t.Fatal(err)
				}
				return c.res, c.err
			}
			if code := w.runWith(); code != c.want {
				t.Fatalf("exit %d: %s", code, w.err.String())
			}
			mustContain(t, "stderr", w.err.String(), "ERROR: ", c.msg)
		})
	}
}

func TestH11TheRunIsBoundedInTime(t *testing.T) {
	w := goodWorld(t)
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !w.bounded {
		t.Fatal("the runner ran without a deadline")
	}
}

// H12: the counts are read and must agree.
func TestH12TheCountsMustAgreeWithTheRequest(t *testing.T) {
	good := func(tests, modules, tracked string) string {
		return "token=" + testToken + "\ntests=" + tests + "\nskipped=0\nmodules=" + modules + "\ntracked=" + tracked + "\n"
	}
	cases := []struct{ name, counts, want string }{
		{"zero tests", good("0", "2", "2"), "ran 0 tests"},
		{"fewer modules than asked", good("5", "1", "2"), "collected 1 module(s), asked for 2"},
		{"more modules than asked", good("5", "3", "2"), "collected 3 module(s), asked for 2"},
		{"another tracked count", good("5", "2", "9"), "tracked=9, expected 2"},
		{"another token", "token=other\ntests=5\nskipped=0\nmodules=2\ntracked=2\n", "carries token"},
		{"no token", "tests=5\nskipped=0\nmodules=2\ntracked=2\n", "carries token"},
		{"not a number", "token=" + testToken + "\ntests=many\nskipped=0\nmodules=2\ntracked=2\n", "tests= is missing or not a number"},
		{"a missing key", "token=" + testToken + "\ntests=5\nskipped=0\nmodules=2\n", "tracked= is missing"},
		{"a repeated key", good("5", "2", "2") + "tests=6\n", "duplicate tests="},
	}
	for _, c := range cases {
		for _, code := range []int{0, 1} {
			t.Run(c.name+" with runner status "+strconv.Itoa(code), func(t *testing.T) {
				w := goodWorld(t)
				w.counts, w.code = c.counts, code
				if got := w.runWith(); got != ExitCannotRun {
					t.Fatalf("exit %d: %s", got, w.err.String())
				}
				mustContain(t, "stderr", w.err.String(), "ERROR: ", c.want)
			})
		}
	}
}

func TestH12ARunnerThatWroteNoFileIsExitTwoNotAPass(t *testing.T) {
	w := goodWorld(t)
	w.answer = func(*world, proc.Cmd) (proc.Result, error) { return proc.Result{Code: 0}, nil }
	if code := w.runWith(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "ERROR: the runner ended 0 but its counts could not be read")
}

// H13: the closing line.
func TestH13AGreenRunSaysWhatRan(t *testing.T) {
	w := goodWorld(t)
	if code := w.runWith(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", w.err.String(), "HELPER-TESTS-OK: Ran 7 tests in 2 modules (2 tracked), 1 skipped")
}

func modulesOf(argv []string) []string {
	var mods []string
	for _, a := range argv {
		if strings.HasPrefix(a, "tests.helpers.") {
			mods = append(mods, a)
		}
	}
	return mods
}
