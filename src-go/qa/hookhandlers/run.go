// Package hookhandlers is the hook-handlers gate: the project's own hooks-daemon handlers
// (.claude/hooks/handlers) pass their tests (Plan 00191 Task 4.6). The handlers import the
// daemon's package, claude_code_hooks_daemon, which only the daemon's own venv carries, and that
// venv has no pytest; the gate needs nothing installed for it alone. Its expectations:
//
//   - K1 The shared root flags (rootargs): -h prints the usage, an unknown argument is exit 2 with
//     the usage, an unresolvable root is exit 2.
//   - K2 QA_HOOK_HANDLERS_EXEMPT set to a reason is the CI workflow's exemption (its checkout
//     has no daemon, which is gitignored): honoured only where GITHUB_ACTIONS is "true", where
//     the gate prints HOOK-HANDLERS-EXEMPT <reason> and passes, starting nothing. Set anywhere
//     else it is exit 2: no machine but CI decides to skip the gate. The decision is the
//     workflow's (.github/workflows/qa.yml), never the gate's.
//   - K3 The daemon is HOOKS_DAEMON_ROOT_DIR when that is set (and must be a directory), else
//     <root>/.claude/hooks-daemon, else, in a linked worktree that has none, the main checkout's
//     (the directory above git's common directory). None is exit 2 naming the fix.
//   - K4 The daemon's interpreter is what its own resolver answers
//     (<daemon>/scripts/lib/resolve_venv.sh python <daemon>), never a venv name guessed here: one
//     absolute path, or exit 2.
//   - K5 That interpreter must import claude_code_hooks_daemon and say its X.Y version, or exit 2.
//   - K6 pytest is the one the interpreter of the SAME X.Y on PATH (python3.11 for a 3.11 venv)
//     imports: the project's own pytest (the CCY image's python3-pytest, the host's from
//     play-python.yml). The directory that holds it is asked of that interpreter. Choosing it by
//     version means the two Pythons are never mixed across versions. None is exit 2 naming
//     where the project installs it.
//   - K7 The run is the daemon's interpreter, isolated (-I: no PYTHONPATH, no user site, no
//     current directory) and writing no bytecode (-B), with pytest's directory APPENDED to its
//     path, so every module the daemon's venv has is the venv's own and only what it lacks
//     (pytest and its own dependencies) comes from there. Entry-point plugins and the caller's
//     PYTEST_ADDOPTS are shut out, and the run is bounded in time.
//   - K8 The verdict is pytest's: 0 with a positive "N passed" count is a pass, printed as
//     HOOK-HANDLERS-OK N test(s) under .claude/hooks/handlers (and ", M skipped" when any were);
//     1 is exit 1; anything else (no tests collected, a usage error, a timeout, a process left
//     behind, a pass with no count) is exit 2.
//
// stdout is the one verdict line; pytest's output and every diagnostic go to stderr.
package hookhandlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-hook-handlers exits with.
type ExitCode int

// The statuses callers read: 0 passed (or exempt in CI), 1 a test failed, 2 the run could not
// complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// testsRoot is what pytest collects, relative to the root.
const testsRoot = ".claude/hooks/handlers"

// launcher appends pytest's directory (argv[1]) to the path and runs pytest on the rest.
const launcher = "import sys; sys.path.append(sys.argv.pop(1)); import pytest; raise SystemExit(pytest.main(sys.argv[1:]))"

// probeDaemon imports the daemon and prints the interpreter's X.Y.
const probeDaemon = "import sys, claude_code_hooks_daemon; print('%d.%d' % sys.version_info[:2])"

// probePytest prints the directory pytest is imported from.
const probePytest = "import os, pytest; print(os.path.dirname(os.path.dirname(os.path.abspath(pytest.__file__))))"

const (
	exemptVar     = "QA_HOOK_HANDLERS_EXEMPT"
	daemonRootVar = "HOOKS_DAEMON_ROOT_DIR"
)

// provisionFix is how a machine gets the daemon.
const provisionFix = "provision the hooks daemon (bash .claude/provision.sh, or the hooks-daemon skill: provision)"

// pytestFix is where the project installs its pytest.
const pytestFix = "the project's pytest is python3-pytest: the CCY image installs it (.claude/ccy/Dockerfile) and the host play playbooks/imports/play-python.yml; do NOT install it by hand"

// defaultTimeout bounds the test run when the Env gives none.
const defaultTimeout = 10 * time.Minute

// probeTimeout bounds each question asked before the run.
const probeTimeout = time.Minute

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks; a test gives its own).
	Resolve rootpath.Eval
	// Getenv reads the environment.
	Getenv func(string) string
	// IsDir says whether a path is a directory.
	IsDir func(string) bool
	// Runner starts every program.
	Runner proc.Runner
	// Timeout bounds the test run; zero is defaultTimeout.
	Timeout time.Duration
}

const usageText = `usage: qa-hook-handlers [--root DIR] [--scripts DIR]

Runs the tests of the project's hooks-daemon handlers (.claude/hooks/handlers/**/tests) under the
daemon's own interpreter, with the project's pytest (the one the interpreter of the daemon's Python
version carries) appended to its path.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

HOOKS_DAEMON_ROOT_DIR      the daemon's directory (default <root>/.claude/hooks-daemon, else the
                           main checkout's)
QA_HOOK_HANDLERS_EXEMPT    the CI workflow's exemption and its reason; refused off GitHub Actions

stdout is the verdict line; pytest's output and diagnostics go to stderr.
Exit codes: 0 passed (or exempt in CI), 1 a test failed, 2 bad usage or the run could not complete.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

// gate is one run's state.
type gate struct {
	env    Env
	root   string
	errOut *emit.Emitter
}

// cannotRunError is a reason the run could not complete.
type cannotRunError struct{ msg string }

func (e *cannotRunError) Error() string { return e.msg }

func cannot(format string, a ...any) error { return &cannotRunError{msg: fmt.Sprintf(format, a...)} }

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
	if reason := strings.TrimSpace(env.Getenv(exemptVar)); reason != "" {
		if env.Getenv("GITHUB_ACTIONS") != "true" {
			errOut.Line("ERROR: " + exemptVar + " is set, but the exemption is honoured only on GitHub Actions, where .github/workflows/qa.yml sets it; unset it and run the gate")
			return ExitCannotRun
		}
		out.Line("HOOK-HANDLERS-EXEMPT " + reason)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.Root, opts.Scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	g := &gate{env: env, root: root, errOut: errOut}
	code, verdict, err := g.check()
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	if verdict != "" {
		out.Line(verdict)
	}
	return code
}

// check finds the daemon, its interpreter and pytest, and runs the tests.
func (g *gate) check() (ExitCode, string, error) {
	daemon, err := g.daemonDir()
	if err != nil {
		return ExitCannotRun, "", err
	}
	python, err := g.interpreter(daemon)
	if err != nil {
		return ExitCannotRun, "", err
	}
	version, err := g.daemonVersion(python)
	if err != nil {
		return ExitCannotRun, "", err
	}
	pytestDir, err := g.pytestDir(version)
	if err != nil {
		return ExitCannotRun, "", err
	}
	return g.runTests(python, pytestDir)
}

// daemonDir is the daemon's directory (K3).
func (g *gate) daemonDir() (string, error) {
	if set := g.env.Getenv(daemonRootVar); set != "" {
		if !g.env.IsDir(set) {
			return "", cannot("%s=%s is not a directory: point it at the daemon, or unset it", daemonRootVar, set)
		}
		return set, nil
	}
	own := filepath.Join(g.root, ".claude", "hooks-daemon")
	if g.env.IsDir(own) {
		return own, nil
	}
	common, err := g.ask([]string{"git", "-C", g.root, "rev-parse", "--path-format=absolute", "--git-common-dir"}, "git cannot say where the main checkout is")
	if err != nil {
		return "", err
	}
	main := filepath.Join(filepath.Dir(common), ".claude", "hooks-daemon")
	if !g.env.IsDir(main) {
		return "", cannot("no hooks daemon at %s or %s: %s", own, main, provisionFix)
	}
	return main, nil
}

// interpreter is the daemon venv's python, as the daemon's resolver answers (K4).
func (g *gate) interpreter(daemon string) (string, error) {
	resolver := filepath.Join(daemon, "scripts", "lib", "resolve_venv.sh")
	python, err := g.ask([]string{resolver, "python", daemon}, "the daemon at "+daemon+" has no usable venv: "+provisionFix)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(python) {
		return "", cannot("the daemon's resolver answered %q, not an absolute interpreter path: %s", python, provisionFix)
	}
	return python, nil
}

// version is an interpreter's X.Y.
var version = regexp.MustCompile(`^3\.[0-9]+$`)

// daemonVersion is the X.Y of an interpreter that imports the daemon (K5).
func (g *gate) daemonVersion(python string) (string, error) {
	got, err := g.ask([]string{python, "-I", "-c", probeDaemon}, python+" cannot import claude_code_hooks_daemon: "+provisionFix)
	if err != nil {
		return "", err
	}
	if !version.MatchString(got) {
		return "", cannot("%s says its version is %q, not 3.N", python, got)
	}
	return got, nil
}

// pytestDir is the directory the same-version interpreter imports pytest from (K6).
func (g *gate) pytestDir(version string) (string, error) {
	python := "python" + version
	dir, err := g.ask([]string{python, "-I", "-c", probePytest}, python+" (the daemon's Python version) cannot import pytest: "+pytestFix)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", cannot("%s says pytest is in %q, not an absolute directory: %s", python, dir, pytestFix)
	}
	return dir, nil
}

// ask runs a question and returns its one line of answer; anything else is a cannotRunError
// that starts with why.
func (g *gate) ask(argv []string, why string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.root})
	switch {
	case err != nil:
		return "", cannot("%s (%s could not start: %v)", why, argv[0], err)
	case res.TimedOut:
		return "", cannot("%s (%s timed out after %s)", why, argv[0], probeTimeout)
	case res.Code != 0:
		return "", cannot("%s (%s exited %d: %s)", why, argv[0], res.Code, strings.TrimSpace(res.Stderr))
	}
	answer := strings.TrimSpace(res.Stdout)
	if answer == "" || strings.Contains(answer, "\n") {
		return "", cannot("%s (%s answered %q, not one line)", why, argv[0], res.Stdout)
	}
	return answer, nil
}

// runTests runs pytest and judges it (K7, K8).
func (g *gate) runTests(python, pytestDir string) (ExitCode, string, error) {
	timeout := g.env.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	argv := []string{python, "-I", "-B", "-c", launcher, pytestDir, testsRoot, "-p", "no:cacheprovider", "-q", "--color=no"}
	res, err := g.env.Runner.Run(ctx, proc.Cmd{
		Argv:  argv,
		Dir:   g.root,
		Env:   []string{"PYTEST_DISABLE_PLUGIN_AUTOLOAD=1", "PYTEST_ADDOPTS="},
		Merge: true,
	})
	if err != nil {
		return ExitCannotRun, "", cannot("the handler tests could not start under %s: %v", python, err)
	}
	g.errOut.Text(res.Stdout)
	switch {
	case res.TimedOut:
		return ExitCannotRun, "", cannot("the handler tests timed out after %s and were killed", timeout)
	case res.Leftover:
		return ExitCannotRun, "", cannot("a handler test left a process holding pytest's output open; the verdict cannot be trusted")
	case res.Code == 1:
		g.errOut.Line("qa-hook-handlers: a handler test failed (pytest exit 1)")
		return ExitFailed, "", nil
	case res.Code != 0:
		return ExitCannotRun, "", cannot("pytest ended %d, which is neither pass (0) nor fail (1): it did not run the tests to a verdict", res.Code)
	}
	passed, skipped := count(res.Stdout, passedCount), count(res.Stdout, skippedCount)
	if passed <= 0 {
		return ExitCannotRun, "", cannot("pytest passed but reported no test passing: a run that proved nothing is not a pass")
	}
	verdict := fmt.Sprintf("HOOK-HANDLERS-OK %d test(s) under %s", passed, testsRoot)
	if skipped > 0 {
		verdict += fmt.Sprintf(", %d skipped", skipped)
	}
	return ExitOK, verdict, nil
}

// The counts pytest's summary line gives.
var (
	passedCount  = regexp.MustCompile(`\b([0-9]+) passed\b`)
	skippedCount = regexp.MustCompile(`\b([0-9]+) skipped\b`)
)

// count is the last number the pattern finds in pytest's output, or 0.
func count(output string, pattern *regexp.Regexp) int {
	m := pattern.FindAllStringSubmatch(output, -1)
	if len(m) == 0 {
		return 0
	}
	n, err := strconv.Atoi(m[len(m)-1][1])
	if err != nil {
		return 0
	}
	return n
}
