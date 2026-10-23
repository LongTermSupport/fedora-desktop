// Package plantests is the plan-tests gate: every active plan folder's own Python tests
// (CLAUDE/Plan/<NNNNN>-*/**/test_*.py) pass. Plan folders hold the checkers and probes of their
// acceptance and triage scripts, and those checkers' tests sit beside them; the helper-tests gate
// reads only tests/helpers, so before this gate nothing ran them. Its expectations:
//
//   - P1 The shared root flags (rootargs): -h prints the usage, an unknown argument is exit 2 with
//     the usage, an unresolvable root is exit 2.
//   - P2 The scope is the regular files (or symlinks to one) named test_*.py anywhere under an
//     active plan folder, a directory of CLAUDE/Plan whose name is digits, a dash and more.
//     Completed/ is out: a completed plan is frozen history whose checkers ran against code that
//     has since moved on, and nobody maintains them; the plan tooling at CLAUDE/Plan's top is
//     not a plan. discovery.IsPlanTest is the predicate, shared with the tracked list.
//   - P3 The walk is measured against git's index (the tracked-plan-tests list): a tracked
//     plan test the walk did not find is exit 2, naming it, before anything runs; an untracked one
//     found runs, and is named. A tree or a tracked list that cannot be read is exit 2.
//   - P4 No plan test found and none tracked is a pass that says so: plans may have none.
//   - P5 Each module is run by name, so a file whose name is not a Python identifier is exit 2.
//   - P6 Each directory holding tests runs in a process of its own, the way the tests expect:
//     the stdlib unittest runner helpers/qa_environment/unittest_counts.py with --chdir, which
//     runs them as `cd DIR && python3 -m unittest <modules>` would (DIR the working directory and
//     the first entry of the import path; the tests load their siblings by __file__ or by name).
//     It starts from the root, so the runner itself imports, with the machine's git
//     configuration kept out, no bytecode written into the plan folder, a fresh token per run
//     and a private counts file, its output streamed to stderr under a header naming the
//     directory, and is bounded in time. A token that cannot be made is exit 2.
//   - P7 A directory's verdict is the runner's exit status checked against the counts it wrote
//     (gates.HelperRun): 0 with tests run, the modules asked for and the tracked count expected
//     is a pass; 1 is a failure, even when the counts also disagree; a runner that could not
//     start, timed out, left a process behind, ended any other way, or wrote counts that cannot
//     be read or disagree on a 0 is a run that reached no verdict.
//   - P8 Every directory runs whatever the ones before it did. Any directory with no verdict is
//     exit 2; else any failed is exit 1, each named on stderr; else the pass line on stdout:
//     PLAN-TESTS-OK <T> test(s) in <M> module(s) in <D> plan folder(s), <S> skipped.
//   - P9 The counts files are removed afterwards.
//
// stdout is the one pass line; the runner's output and every diagnostic go to stderr.
package plantests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-plan-tests exits with.
type ExitCode int

// The statuses callers read: 0 every test passed, 1 a test failed, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// planDir holds the plan folders, relative to the root.
const planDir = "CLAUDE/Plan"

// defaultTimeout bounds each directory's run when the Env gives none.
const defaultTimeout = 10 * time.Minute

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks; a test gives its own).
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// Tracked lists the tracked plan tests, relative to root (the shared discovery's
	// tracked-plan-tests list: git's index, less deleted files). Paths outside the scope are
	// ignored.
	Tracked func(root string) ([]string, error)
	// Runner starts the unittest runner.
	Runner proc.Runner
	// TempDir is where the counts files are made ("" is the system's).
	TempDir string
	// Token makes a fresh counts token.
	Token func() (string, error)
	// Timeout bounds each directory's run; zero is defaultTimeout.
	Timeout time.Duration
}

const usageText = `usage: qa-plan-tests [--root DIR] [--scripts DIR]

Runs every active plan folder's own Python tests (CLAUDE/Plan/<NNNNN>-*/**/test_*.py; Completed/
is frozen history and is not run), each directory in a process of its own, from that directory,
under the stdlib unittest runner that records how many tests ran.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

stdout is the pass line; the tests' output and diagnostics go to stderr.
Exit codes: 0 every test passed, 1 a test failed, 2 bad usage or the run could not complete.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := rootargs.Strict(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if opts.Help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.Root, opts.Scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	groups, ok := scope(env, root, errOut)
	if !ok {
		return ExitCannotRun
	}
	if len(groups) == 0 {
		out.Line("PLAN-TESTS-OK no test files in the active plan folders")
		return ExitOK
	}
	return runAll(env, root, groups, out, errOut)
}

// group is one directory's tests.
type group struct {
	dir     string
	modules []string
	tracked int
}

// scope is the directories to run, in order, or false after saying why (P2-P5).
func scope(env Env, root string, errOut *emit.Emitter) ([]group, bool) {
	found, err := walk(env.Open(root))
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return nil, false
	}
	all, err := env.Tracked(root)
	if err != nil {
		errOut.Line("ERROR: the tracked files cannot be listed: " + err.Error())
		return nil, false
	}
	var tracked []string
	for _, rel := range all {
		if discovery.IsPlanTest(rel) {
			tracked = append(tracked, rel)
		}
	}
	slices.Sort(tracked)
	tracked = slices.Compact(tracked)
	if missed := difference(tracked, slices.Sorted(slices.Values(found))); len(missed) > 0 {
		errOut.Line(fmt.Sprintf("ERROR: discovery missed %d tracked plan test file(s):", len(missed)))
		for _, rel := range missed {
			errOut.Line("    " + rel)
		}
		errOut.Line("  These are tests this gate would have reported a pass over without")
		errOut.Line("  running. Fix the discovery; do not untrack the files to silence it.")
		return nil, false
	}
	untracked := difference(found, tracked)
	if len(untracked) > 0 {
		errOut.Line(fmt.Sprintf("plan tests: %d UNTRACKED file(s), which run here and nowhere else:", len(untracked)))
		for _, rel := range untracked {
			errOut.Line("    " + rel)
		}
	}
	return groupByDir(found, untracked, errOut)
}

// identifier is a module name the runner can import.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// groupByDir turns the sorted paths into one group per directory (P5).
func groupByDir(found, untracked []string, errOut *emit.Emitter) ([]group, bool) {
	var groups []group
	for _, rel := range found {
		dir, name := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		module := strings.TrimSuffix(name, ".py")
		if !identifier.MatchString(module) {
			errOut.Line("ERROR: " + rel + " cannot be imported by name: " + strconv.Quote(module) + " is not a Python identifier. Rename it; a test the runner cannot import is a test nothing runs")
			return nil, false
		}
		if len(groups) == 0 || groups[len(groups)-1].dir != dir {
			groups = append(groups, group{dir: dir})
		}
		g := &groups[len(groups)-1]
		g.modules = append(g.modules, module)
		if _, isUntracked := slices.BinarySearch(untracked, rel); !isUntracked {
			g.tracked++
		}
	}
	return groups, true
}

// difference is the members of a that b lacks; both sorted.
func difference(a, b []string) []string {
	var out []string
	for _, rel := range a {
		if _, found := slices.BinarySearch(b, rel); !found {
			out = append(out, rel)
		}
	}
	return out
}

// walk is every plan test on disk, repository-relative, sorted so a directory's tests are
// adjacent (P2).
func walk(fsys fs.FS) ([]string, error) {
	var found []string
	err := fs.WalkDir(fsys, planDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", p, err)
		}
		if d.IsDir() {
			if path.Dir(p) == planDir && !discovery.ActivePlan.MatchString(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		keep, keepErr := regularPlanTest(fsys, p, d)
		if keep {
			found = append(found, p)
		}
		return keepErr
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b string) int {
		if c := strings.Compare(path.Dir(a), path.Dir(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return found, nil
}

// regularPlanTest is whether a file the walk met is a plan test: named so, and a regular file or a
// symlink to one (P2).
func regularPlanTest(fsys fs.FS, p string, d fs.DirEntry) (bool, error) {
	if !discovery.IsPlanTest(p) {
		return false, nil
	}
	if d.Type()&fs.ModeSymlink == 0 {
		return d.Type().IsRegular(), nil
	}
	info, err := fs.Stat(fsys, p)
	if err != nil {
		return false, fmt.Errorf("the symlink %s cannot be followed: %w", p, err)
	}
	return info.Mode().IsRegular(), nil
}

// verdict is how one directory ended.
type verdict int

const (
	passed verdict = iota
	failed
	noVerdict
)

// tally is the sum of the passing directories' counts.
type tally struct{ tests, skipped, modules int }

// runAll runs every directory and judges the whole (P8).
func runAll(env Env, root string, groups []group, out, errOut *emit.Emitter) ExitCode {
	var sum tally
	var failedDirs, blindDirs []string
	for _, g := range groups {
		v, n := runGroup(env, root, g, errOut)
		switch v {
		case passed:
			sum.tests += n.Tests
			sum.skipped += n.Skipped
			sum.modules += n.Modules
		case failed:
			failedDirs = append(failedDirs, g.dir)
		case noVerdict:
			blindDirs = append(blindDirs, g.dir)
		}
	}
	for _, dir := range failedDirs {
		errOut.Line("ERROR: plan tests failed in " + dir)
	}
	for _, dir := range blindDirs {
		errOut.Line("ERROR: plan tests reached no verdict in " + dir)
	}
	switch {
	case len(blindDirs) > 0:
		errOut.Line(fmt.Sprintf("ERROR: %d of %d plan folder(s) reached no verdict, %d failed", len(blindDirs), len(groups), len(failedDirs)))
		return ExitCannotRun
	case len(failedDirs) > 0:
		errOut.Line(fmt.Sprintf("ERROR: %d of %d plan folder(s) failed", len(failedDirs), len(groups)))
		return ExitFailed
	}
	out.Line(fmt.Sprintf("PLAN-TESTS-OK %d test(s) in %d module(s) in %d plan folder(s), %d skipped", sum.tests, sum.modules, len(groups), sum.skipped))
	return ExitOK
}

// runGroup runs one directory's tests and judges them (P6, P7, P9).
func runGroup(env Env, root string, g group, errOut *emit.Emitter) (verdict, gates.HelperNumbers) {
	none := gates.HelperNumbers{}
	token, err := env.Token()
	if err != nil {
		errOut.Line("ERROR: " + g.dir + ": cannot make a counts token: " + err.Error())
		return noVerdict, none
	}
	f, err := os.CreateTemp(env.TempDir, "qa-plan-tests-counts-*")
	if err != nil {
		errOut.Line("ERROR: " + g.dir + ": cannot make a counts file: " + err.Error())
		return noVerdict, none
	}
	counts := f.Name()
	if err := f.Close(); err != nil {
		errOut.Line("ERROR: " + g.dir + ": cannot make a counts file: " + errors.Join(err, os.Remove(counts)).Error())
		return noVerdict, none
	}
	v, n := execute(env, root, g, counts, token, errOut)
	if err := os.Remove(counts); err != nil {
		errOut.Line("ERROR: " + g.dir + ": cannot remove the counts file: " + err.Error())
		return noVerdict, none
	}
	return v, n
}

// execute starts the runner for one directory and judges what it left.
func execute(env Env, root string, g group, counts, token string, errOut *emit.Emitter) (verdict, gates.HelperNumbers) {
	none := gates.HelperNumbers{}
	argv := make([]string, 0, 19+len(g.modules))
	argv = append(argv,
		"env", "-u", "GIT_CONFIG_COUNT", "-u", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"python3", "-B", "-m", "helpers.qa_environment.unittest_counts",
		"--chdir", filepath.Join(root, filepath.FromSlash(g.dir)),
		"--counts-file", counts, "--counts-token", token,
		"--tracked-modules", strconv.Itoa(g.tracked),
	)
	argv = append(argv, g.modules...)
	errOut.Line(fmt.Sprintf("== %s: %d module(s)", g.dir, len(g.modules)))
	timeout := env.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: root, StdoutTo: errOut, StderrTo: errOut})
	switch {
	case err != nil:
		errOut.Line("ERROR: " + g.dir + ": the unittest runner could not be started: " + err.Error() + ". python3 is installed by playbooks/imports/play-python.yml; do NOT install it by hand")
		return noVerdict, none
	case res.TimedOut:
		errOut.Line("ERROR: " + g.dir + ": the unittest runner timed out after " + timeout.String() + " and was killed")
		return noVerdict, none
	case res.Leftover:
		errOut.Line("ERROR: " + g.dir + ": a test left a process holding the runner's output open; the verdict cannot be trusted")
		return noVerdict, none
	case res.Code != 0 && res.Code != 1:
		errOut.Line(fmt.Sprintf("ERROR: %s: the runner ended %d, which is neither pass (0) nor fail (1): it did not run to a verdict", g.dir, res.Code))
		return noVerdict, none
	}
	return judge(res.Code, counts, token, g, errOut)
}

// judge reads the counts the runner wrote and checks them against the request (P7).
func judge(status int, counts, token string, g group, errOut *emit.Emitter) (verdict, gates.HelperNumbers) {
	n, err := gates.HelperRun(counts, token)
	if err != nil {
		errOut.Line(fmt.Sprintf("ERROR: %s: the runner ended %d but its counts could not be read: %v", g.dir, status, err))
		return noVerdict, n
	}
	var problem string
	switch {
	case n.Tests == 0:
		problem = "the runner ran 0 tests: a run that collected nothing is not a pass"
	case n.Modules != len(g.modules):
		problem = fmt.Sprintf("the runner collected %d module(s), asked for %d", n.Modules, len(g.modules))
	case n.Tracked != g.tracked:
		problem = fmt.Sprintf("the counts say tracked=%d, expected %d", n.Tracked, g.tracked)
	}
	if problem != "" {
		errOut.Line(fmt.Sprintf("ERROR: %s: the runner ended %d, but %s", g.dir, status, problem))
		if status == 1 {
			return failed, n
		}
		return noVerdict, n
	}
	if status == 1 {
		errOut.Line(fmt.Sprintf("ERROR: %s: plan tests failed (%d run, %d skipped)", g.dir, n.Tests, n.Skipped))
		return failed, n
	}
	return passed, n
}
