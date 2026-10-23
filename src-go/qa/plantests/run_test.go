package plantests

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const repo = "/repo"

const (
	dirA = "CLAUDE/Plan/00161-a"
	dirB = "CLAUDE/Plan/00203-b"
)

// outcome is what the fake runner does for one plan directory.
type outcome struct {
	code     int
	tests    int
	skipped  int
	modules  int // -1: the number asked for
	tracked  int // -1: the number asked for
	noCounts bool
	badToken bool
	timedOut bool
	leftover bool
	startErr error
}

func ok(tests int) outcome { return outcome{tests: tests, modules: -1, tracked: -1} }

// world is a fake machine: a tree, git's tracked list, and what each directory's run does.
type world struct {
	t        *testing.T
	tree     fstest.MapFS
	tracked  []string
	trackErr error
	outcomes map[string]outcome
	ran      []proc.Cmd
	bounded  []bool
	tmp      string
	tokens   int
	out, err strings.Builder
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return &world{t: t, tree: fstest.MapFS{}, outcomes: map[string]outcome{}, tmp: t.TempDir()}
}

func flag(argv []string, name string) string {
	i := slices.Index(argv, name)
	if i < 0 || i+1 >= len(argv) {
		return ""
	}
	return argv[i+1]
}

func (w *world) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	w.ran = append(w.ran, c)
	_, bounded := ctx.Deadline()
	w.bounded = append(w.bounded, bounded)
	dir := strings.TrimPrefix(flag(c.Argv, "--chdir"), repo+"/")
	o, given := w.outcomes[dir]
	if !given {
		o = ok(3)
	}
	if o.startErr != nil {
		return proc.Result{}, o.startErr
	}
	asked := len(c.Argv) - slices.Index(c.Argv, "--tracked-modules") - 2
	modules, tracked := o.modules, o.tracked
	if modules < 0 {
		modules = asked
	}
	if tracked < 0 {
		tracked = asked
		if n := flag(c.Argv, "--tracked-modules"); n != "" {
			_, err := fmt.Sscanf(n, "%d", &tracked)
			if err != nil {
				w.t.Fatal(err)
			}
		}
	}
	if !o.noCounts {
		token := flag(c.Argv, "--counts-token")
		if o.badToken {
			token = "forged"
		}
		body := fmt.Sprintf("token=%s\ntests=%d\nskipped=%d\nmodules=%d\ntracked=%d\n", token, o.tests, o.skipped, modules, tracked)
		if err := os.WriteFile(flag(c.Argv, "--counts-file"), []byte(body), 0o600); err != nil {
			w.t.Fatal(err)
		}
	}
	if c.StderrTo != nil {
		if _, err := fmt.Fprintf(c.StderrTo, "unittest output for %s\n", dir); err != nil {
			w.t.Fatal(err)
		}
	}
	return proc.Result{Code: o.code, TimedOut: o.timedOut, Leftover: o.leftover}, nil
}

// test adds a tracked test file; its directory passes unless an outcome says otherwise.
func (w *world) test(rel string) {
	w.tree[rel] = &fstest.MapFile{Data: []byte("import unittest\n")}
	w.tracked = append(w.tracked, rel)
}

func (w *world) env() Env {
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     repo,
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return w.tree },
		Tracked: func(string) ([]string, error) { return w.tracked, w.trackErr },
		Runner:  w,
		TempDir: w.tmp,
		Token: func() (string, error) {
			w.tokens++
			return fmt.Sprintf("tok%d", w.tokens), nil
		},
	}
}

func (w *world) run(t *testing.T, want ExitCode, args ...string) {
	t.Helper()
	if got := Main(args, w.env()); got != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, w.out.String(), w.err.String())
	}
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func twoFolders(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.test(dirA + "/test_one.py")
	w.test(dirA + "/test_two.py")
	w.test(dirB + "/test_three.py")
	return w
}

// P1: the shared root flags.
func TestP1RootFlags(t *testing.T) {
	w := newWorld(t)
	w.run(t, ExitOK, "--help")
	mustContain(t, w.out.String(), "usage: qa-plan-tests")
	w = newWorld(t)
	w.run(t, ExitCannotRun, "--bogus")
	mustContain(t, w.err.String(), "unknown argument", "usage: qa-plan-tests")
	w = newWorld(t)
	env := w.env()
	env.Resolve = func(string) (string, error) { return "", errors.New("no such directory") }
	if got := Main([]string{"--root", "/gone"}, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
}

// P2, P6, P8: each plan folder runs in its own process, from that folder, and the pass line
// adds them up.
func TestP6EachFolderRunsFromItself(t *testing.T) {
	w := twoFolders(t)
	w.run(t, ExitOK)
	if got := w.out.String(); got != "PLAN-TESTS-OK 6 test(s) in 3 module(s) in 2 plan folder(s), 0 skipped\n" {
		t.Fatalf("stdout %q", got)
	}
	if len(w.ran) != 2 {
		t.Fatalf("ran %d processes, want 2", len(w.ran))
	}
	wantA := []string{
		"env", "-u", "GIT_CONFIG_COUNT", "-u", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "PYTHONDONTWRITEBYTECODE=1",
		"python3", "-B", "-m", "helpers.qa_environment.unittest_counts",
		"--chdir", repo + "/" + dirA,
		"--counts-file", flag(w.ran[0].Argv, "--counts-file"),
		"--counts-token", "tok1",
		"--tracked-modules", "2",
		"test_one", "test_two",
	}
	if !slices.Equal(w.ran[0].Argv, wantA) {
		t.Fatalf("argv %q\nwant %q", w.ran[0].Argv, wantA)
	}
	if flag(w.ran[1].Argv, "--chdir") != repo+"/"+dirB || flag(w.ran[1].Argv, "--counts-token") != "tok2" {
		t.Fatalf("second run %q", w.ran[1].Argv)
	}
	for i, c := range w.ran {
		if c.Dir != repo || c.StdoutTo == nil || c.StderrTo == nil || !w.bounded[i] {
			t.Fatalf("run %d: dir %q, streamed %v/%v, bounded %v", i, c.Dir, c.StdoutTo != nil, c.StderrTo != nil, w.bounded[i])
		}
	}
	mustContain(t, w.err.String(), "== "+dirA+": 2 module(s)", "unittest output for "+dirA, "== "+dirB+": 1 module(s)")
}

// P9: the counts files are private and removed afterwards.
func TestP9CountsFilesAreRemoved(t *testing.T) {
	w := twoFolders(t)
	w.run(t, ExitOK)
	left, err := os.ReadDir(w.tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	if !strings.HasPrefix(flag(w.ran[0].Argv, "--counts-file"), w.tmp+"/") {
		t.Fatalf("counts file %q not in the temp dir", flag(w.ran[0].Argv, "--counts-file"))
	}
}

// P2: Completed/, the plan tooling, files not named test_*.py and non-regular files are out of
// scope; a test in a subdirectory of an active plan runs from that subdirectory.
func TestP2Scope(t *testing.T) {
	w := newWorld(t)
	w.test(dirA + "/test_one.py")
	w.test(dirA + "/sub/test_deep.py")
	w.tree["CLAUDE/Plan/Completed/00100-old/test_old.py"] = &fstest.MapFile{Data: []byte("x")}
	w.tree["CLAUDE/Plan/test_tooling.py"] = &fstest.MapFile{Data: []byte("x")}
	w.tree["CLAUDE/Plan/notaplan/test_x.py"] = &fstest.MapFile{Data: []byte("x")}
	w.tree[dirA+"/helper_test.py"] = &fstest.MapFile{Data: []byte("x")}
	w.tree[dirA+"/test_data.json"] = &fstest.MapFile{Data: []byte("x")}
	w.tree[dirA+"/test_dir.py"] = &fstest.MapFile{Mode: fs.ModeDir}
	w.run(t, ExitOK)
	if len(w.ran) != 2 {
		t.Fatalf("ran %d processes: %v", len(w.ran), w.ran)
	}
	if got := flag(w.ran[1].Argv, "--chdir"); got != repo+"/"+dirA+"/sub" {
		t.Fatalf("second run from %q", got)
	}
	if !slices.Equal(w.ran[0].Argv[len(w.ran[0].Argv)-1:], []string{"test_one"}) {
		t.Fatalf("first run %q", w.ran[0].Argv)
	}
}

// P3: a tracked path outside the scope does not count as a plan test the walk missed.
func TestP3TrackedPathsOutsideTheScopeAreIgnored(t *testing.T) {
	w := twoFolders(t)
	w.tracked = append(w.tracked, "CLAUDE/Plan/Completed/00100-old/test_x.py", "README")
	w.run(t, ExitOK)
}

// P3: a tracked plan test the walk did not find is exit 2; an untracked one runs and is named.
func TestP3TrackedTestTheWalkMissedIsExit2(t *testing.T) {
	w := twoFolders(t)
	w.tracked = append(w.tracked, dirB+"/test_lost.py")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "discovery missed 1 tracked plan test file(s)", dirB+"/test_lost.py")
	if len(w.ran) != 0 {
		t.Fatalf("ran %v", w.ran)
	}
}

func TestP3UntrackedTestRunsAndIsNamed(t *testing.T) {
	w := twoFolders(t)
	w.tree[dirB+"/test_new.py"] = &fstest.MapFile{Data: []byte("x")}
	w.run(t, ExitOK)
	mustContain(t, w.err.String(), "1 UNTRACKED", dirB+"/test_new.py")
	if got := flag(w.ran[1].Argv, "--tracked-modules"); got != "1" {
		t.Fatalf("tracked-modules %q, want 1", got)
	}
}

func TestP3TrackedListFailureIsExit2(t *testing.T) {
	w := twoFolders(t)
	w.trackErr = errors.New("git broke")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "git broke")
}

func TestP3UnreadableTreeIsExit2(t *testing.T) {
	w := newWorld(t)
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "cannot read")
}

// P4: no plan test anywhere, tracked or found, is a pass that says so.
func TestP4NoPlanTestsIsAPassThatSaysSo(t *testing.T) {
	w := newWorld(t)
	w.tree["CLAUDE/Plan/00161-a/PLAN.md"] = &fstest.MapFile{Data: []byte("x")}
	w.run(t, ExitOK)
	if got := w.out.String(); got != "PLAN-TESTS-OK no test files in the active plan folders\n" {
		t.Fatalf("stdout %q", got)
	}
	if len(w.ran) != 0 {
		t.Fatalf("ran %v", w.ran)
	}
}

// P5: a test file whose name is not an identifier cannot be run by name: exit 2.
func TestP5ModuleNameMustBeAnIdentifier(t *testing.T) {
	w := newWorld(t)
	w.test(dirA + "/test_bad-name.py")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "test_bad-name.py cannot be imported by name")
}

// P7, P8: a failing folder is exit 1, named, and the folders after it still run.
func TestP7AFailingFolderIsExit1AndTheRestStillRun(t *testing.T) {
	w := twoFolders(t)
	w.outcomes[dirA] = outcome{code: 1, tests: 68, modules: -1, tracked: -1}
	w.run(t, ExitFailed)
	if len(w.ran) != 2 {
		t.Fatalf("ran %d, want both folders", len(w.ran))
	}
	if w.out.String() != "" {
		t.Fatalf("stdout %q on a failure", w.out.String())
	}
	mustContain(t, w.err.String(), "ERROR: plan tests failed in "+dirA, "ERROR: 1 of 2 plan folder(s) failed")
}

// P7: a red runner whose counts disagree is still a failure, not hidden as cannot-run.
func TestP7ARedRunWithDisagreeingCountsIsStillAFailure(t *testing.T) {
	w := twoFolders(t)
	w.outcomes[dirA] = outcome{code: 1, tests: 1, modules: 9, tracked: -1}
	w.run(t, ExitFailed)
	mustContain(t, w.err.String(), "collected 9 module(s), asked for 2")
}

// P7: every way a run can fail to reach a verdict is exit 2, and outranks a failure elsewhere.
func TestP7RunsThatReachNoVerdictAreExit2(t *testing.T) {
	cases := map[string]struct {
		o    outcome
		want string
	}{
		"start error":   {outcome{startErr: errors.New("no python3")}, "could not be started"},
		"timeout":       {outcome{timedOut: true, modules: -1, tracked: -1, tests: 1}, "timed out"},
		"leftover":      {outcome{leftover: true, modules: -1, tracked: -1, tests: 1}, "left a process"},
		"odd exit":      {outcome{code: 2, modules: -1, tracked: -1, tests: 1}, "neither pass (0) nor fail (1)"},
		"no counts":     {outcome{noCounts: true}, "counts could not be read"},
		"forged token":  {outcome{badToken: true, tests: 1, modules: -1, tracked: -1}, "counts could not be read"},
		"zero tests":    {outcome{tests: 0, modules: -1, tracked: -1}, "ran 0 tests"},
		"wrong modules": {outcome{tests: 3, modules: 1, tracked: -1}, "collected 1 module(s), asked for 2"},
		"wrong tracked": {outcome{tests: 3, modules: -1, tracked: 5}, "tracked=5, expected 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := twoFolders(t)
			w.outcomes[dirA] = c.o
			w.outcomes[dirB] = outcome{code: 1, tests: 1, modules: -1, tracked: -1}
			w.run(t, ExitCannotRun)
			mustContain(t, w.err.String(), c.want, dirA)
			if len(w.ran) != 2 {
				t.Fatalf("ran %d, want both folders", len(w.ran))
			}
		})
	}
}

// P8: skips are counted in the pass line.
func TestP8SkipsAreCounted(t *testing.T) {
	w := twoFolders(t)
	w.outcomes[dirB] = outcome{tests: 2, skipped: 2, modules: -1, tracked: -1}
	w.run(t, ExitOK)
	if got := w.out.String(); got != "PLAN-TESTS-OK 5 test(s) in 3 module(s) in 2 plan folder(s), 2 skipped\n" {
		t.Fatalf("stdout %q", got)
	}
}

// P6: a token that cannot be made is exit 2 before anything runs.
func TestP6TokenFailureIsExit2(t *testing.T) {
	w := twoFolders(t)
	env := w.env()
	env.Token = func() (string, error) { return "", errors.New("no entropy") }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, w.err.String(), "no entropy")
}
