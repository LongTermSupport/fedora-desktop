// Package helpertests is the helper unit-test gate: it finds every tests/helpers/**/test_*.py,
// proves against git's index that none was lost, runs them all in one call to the stdlib unittest
// runner (helpers/qa_environment/unittest_counts.py, which writes the run's size and skip count to
// a file as data) and refuses to call the run a pass unless that file is the one this run wrote and
// agrees with what was asked. The specification is the plan's SPEC-qa-helper-tests.md.
package helpertests

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

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-helper-tests exits with.
type ExitCode int

// The statuses callers read: 0 every test passed, 1 a test failed, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// testsDir is where the helper tests live, relative to the root.
const testsDir = "tests/helpers"

// defaultTimeout bounds the runner when the Env gives none.
const defaultTimeout = 30 * time.Minute

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given, and the directory a
	// relative --counts-file is relative to.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks; a test on a fake tree gives
	// its own).
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// Tracked lists, relative to root, the helper tests git tracks (the shared discovery's
	// tracked-helper-tests list).
	Tracked func(root string) ([]string, error)
	// Runner starts the unittest runner.
	Runner proc.Runner
	// Timeout bounds the runner; zero is defaultTimeout.
	Timeout time.Duration
}

const usageText = `usage: qa-helper-tests [--root DIR] [--scripts DIR] [--counts-file PATH] [--counts-token VALUE]

Runs every tests/helpers/**/test_*.py under the stdlib unittest runner and records the run's size
and skip count, as data, in the counts file (default: a private file that is removed afterwards).

  --root DIR            the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR         the scripts directory; the root is its parent
  --counts-file PATH    where the runner writes tests, skipped, modules and tracked
  --counts-token VALUE  echoed into the counts file and required to come back
  -h, --help            this text

Everything but this usage goes to stderr; stdout stays empty on a run.
Exit codes: 0 every test passed, 1 a test failed, 2 bad usage or the run could not complete.
`

// options is the parsed command line.
type options struct {
	help                                   bool
	root, scripts, countsFile, countsToken string
}

// unknownArgError is an argument the tool does not know; the usage text follows it.
type unknownArgError struct{ arg string }

func (e *unknownArgError) Error() string { return fmt.Sprintf("unknown argument '%s'", e.arg) }

func parseArgs(args []string) (options, error) {
	var opts options
	targets := map[string]*string{
		"--root": &opts.root, "--scripts": &opts.scripts,
		"--counts-file": &opts.countsFile, "--counts-token": &opts.countsToken,
	}
	given := map[string]bool{}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		if arg == "-h" || arg == "--help" {
			opts.help = true
			continue
		}
		target, known := targets[arg]
		if !known {
			return options{}, &unknownArgError{arg: arg}
		}
		if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
			return options{}, fmt.Errorf("%s needs a value", arg)
		}
		if given[arg] {
			return options{}, fmt.Errorf("%s given twice", arg)
		}
		given[arg] = true
		*target = rest[0]
		rest = rest[1:]
	}
	return opts, nil
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*unknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if opts.help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.root, opts.scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	modules, tracked, ok := scope(env, root, errOut)
	if !ok {
		return ExitCannotRun
	}
	return execute(opts, env, root, modules, tracked, out, errOut)
}

// scope is the dotted module names to run (sorted) and the count of tracked ones, or false after
// saying why the scope cannot be trusted (H4-H7).
func scope(env Env, root string, errOut *emit.Emitter) ([]string, int, bool) {
	found, err := walk(env.Open(root))
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return nil, 0, false
	}
	if len(found) == 0 {
		errOut.Line("ERROR: found 0 helper tests under " + testsDir + ": this repository always has some, so discovery is broken, and a pass here would vouch for tests nothing had run")
		return nil, 0, false
	}
	tracked, err := env.Tracked(root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return nil, 0, false
	}
	slices.Sort(tracked)
	tracked = slices.Compact(tracked)
	if missed := difference(tracked, found); len(missed) > 0 {
		errOut.Line(fmt.Sprintf("ERROR: discovery missed %d tracked helper test file(s):", len(missed)))
		for _, rel := range missed {
			errOut.Line("    " + rel)
		}
		errOut.Line("  These are tests this gate would have reported a pass over without")
		errOut.Line("  running. Fix the discovery — do not untrack the files to silence it.")
		return nil, 0, false
	}
	untracked := difference(found, tracked)
	errOut.Line(fmt.Sprintf("COVERAGE: %d of %d tracked helper test modules", len(found)-len(untracked), len(tracked)))
	if len(untracked) > 0 {
		errOut.Line(fmt.Sprintf("  plus %d UNTRACKED file(s), which run here and nowhere else:", len(untracked)))
		for _, rel := range untracked {
			errOut.Line("    " + rel)
		}
		errOut.Line("  Commit them or remove them — an uncommitted test makes this run's counts")
		errOut.Line("  incomparable with any other machine's.")
	}
	modules, ok := moduleNames(found, errOut)
	return modules, len(tracked), ok
}

// identifier is one segment of a dotted module name the runner can import.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// moduleNames turns paths into dotted module names (H7).
func moduleNames(paths []string, errOut *emit.Emitter) ([]string, bool) {
	modules := make([]string, 0, len(paths))
	for _, rel := range paths {
		dotted := strings.Split(strings.TrimSuffix(rel, ".py"), "/")
		for _, segment := range dotted {
			if !identifier.MatchString(segment) {
				errOut.Line("ERROR: " + rel + " cannot be imported by name: " + strconv.Quote(segment) + " is not a Python identifier. Rename it; a test the runner cannot import is a test nothing runs")
				return nil, false
			}
		}
		modules = append(modules, strings.Join(dotted, "."))
	}
	return modules, true
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

// walk is every helper test under tests/helpers, repository-relative and sorted (H4).
func walk(fsys fs.FS) ([]string, error) {
	var found []string
	err := fs.WalkDir(fsys, testsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", p, err)
		}
		name := d.Name()
		if d.IsDir() || !strings.HasPrefix(name, "test_") || path.Ext(name) != ".py" {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			info, statErr := fs.Stat(fsys, p)
			if statErr != nil {
				return fmt.Errorf("the symlink %s cannot be followed: %w", p, statErr)
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		} else if !d.Type().IsRegular() {
			return nil
		}
		found = append(found, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(found)
	return found, nil
}

// execute prepares the counts file, runs the suite and judges it (H8-H13).
func execute(opts options, env Env, root string, modules []string, tracked int, out, errOut *emit.Emitter) ExitCode {
	counts, cleanup, err := countsFile(opts.countsFile, env.Cwd)
	if err != nil {
		errOut.Line("ERROR: cannot prepare the counts file: " + err.Error())
		return ExitCannotRun
	}
	code := suite(opts, env, root, counts, modules, tracked, out, errOut)
	if err := cleanup(); err != nil {
		errOut.Line("ERROR: cannot remove the private counts file: " + err.Error())
		return ExitCannotRun
	}
	return code
}

// suite starts the runner and judges what it left.
func suite(opts options, env Env, root, counts string, modules []string, tracked int, out, errOut *emit.Emitter) ExitCode {
	argv := []string{
		"env", "-u", "GIT_CONFIG_COUNT", "-u", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"python3", "-m", "helpers.qa_environment.unittest_counts",
		"--counts-file", counts, "--tracked-modules", strconv.Itoa(tracked),
	}
	if opts.countsToken != "" {
		argv = append(argv, "--counts-token", opts.countsToken)
	}
	argv = append(argv, modules...)
	errOut.Line(fmt.Sprintf("Running %d helper test module(s)...", len(modules)))

	timeout := env.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: root, StdoutTo: out, StderrTo: errOut})
	switch {
	case err != nil:
		errOut.Line("ERROR: the unittest runner could not be started: " + err.Error() + ". python3 is installed by playbooks/imports/play-python.yml; do NOT install it by hand")
		return ExitCannotRun
	case res.TimedOut:
		errOut.Line("ERROR: the unittest runner timed out after " + timeout.String() + " and was killed")
		return ExitCannotRun
	case res.Leftover:
		errOut.Line("ERROR: a test left a process holding the runner's output open; the run's verdict cannot be trusted")
		return ExitCannotRun
	case res.Code != 0 && res.Code != 1:
		errOut.Line(fmt.Sprintf("ERROR: the runner ended %d, which is neither pass (0) nor fail (1): it did not run to a verdict (python3 or env missing, a signal, an argument error)", res.Code))
		return ExitCannotRun
	}
	return judge(res.Code, counts, opts.countsToken, len(modules), tracked, errOut)
}

// judge reads the counts the runner wrote and checks them against the request (H12, H13).
func judge(status int, counts, token string, modules, tracked int, errOut *emit.Emitter) ExitCode {
	n, err := gates.HelperRun(counts, token)
	if err != nil {
		errOut.Line(fmt.Sprintf("ERROR: the runner ended %d but its counts could not be read: %v", status, err))
		return ExitCannotRun
	}
	var problem string
	switch {
	case n.Tests == 0:
		problem = "the runner ran 0 tests: a run that collected nothing is not a pass"
	case n.Modules != modules:
		problem = fmt.Sprintf("the runner collected %d module(s), asked for %d", n.Modules, modules)
	case n.Tracked != tracked:
		problem = fmt.Sprintf("the counts say tracked=%d, expected %d", n.Tracked, tracked)
	}
	if problem != "" {
		errOut.Line(fmt.Sprintf("ERROR: the runner ended %d, but %s", status, problem))
		if status == 1 {
			// The runner said red and its counts are readable: the suite failed, and the
			// disagreement is a second fact about that failure, not a reason to hide it.
			errOut.Line(fmt.Sprintf("ERROR: helper tests failed (%d run, %d skipped)", n.Tests, n.Skipped))
			return ExitFailed
		}
		return ExitCannotRun
	}
	if status == 1 {
		errOut.Line(fmt.Sprintf("ERROR: helper tests failed (%d run, %d skipped)", n.Tests, n.Skipped))
		return ExitFailed
	}
	errOut.Line(fmt.Sprintf("HELPER-TESTS-OK: Ran %d tests in %d modules (%d tracked), %d skipped", n.Tests, n.Modules, n.Tracked, n.Skipped))
	return ExitOK
}

// countsFile is the file the runner writes its counts to, emptied, and what removes it: the given
// path made absolute, else a private temporary file (H9).
func countsFile(given, cwd string) (string, func() error, error) {
	none := func() error { return nil }
	if given == "" {
		f, err := os.CreateTemp("", "qa-helper-counts-*")
		if err != nil {
			return "", none, err
		}
		name := f.Name()
		if err := f.Close(); err != nil {
			return "", none, errors.Join(err, os.Remove(name))
		}
		return name, func() error { return os.Remove(name) }, nil
	}
	if !filepath.IsAbs(given) {
		given = filepath.Join(cwd, given)
	}
	given = filepath.Clean(given)
	if err := os.WriteFile(given, nil, 0o600); err != nil {
		return "", none, err
	}
	return given, none, nil
}
