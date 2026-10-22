package hookhandlers

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	repo       = "/repo"
	daemonDir  = "/repo/.claude/hooks-daemon"
	resolver   = daemonDir + "/scripts/lib/resolve_venv.sh"
	daemonPy   = daemonDir + "/untracked/venv-x/bin/python"
	pytestPath = "/usr/lib/python3/dist-packages"
	passOutput = "....\n101 passed in 0.17s\n"
)

// world is a fake machine: the directories that exist, the environment, and what each program
// answers. A program left out of answers answers as a provisioned machine would.
type world struct {
	dirs    map[string]bool
	vars    map[string]string
	answers map[string]proc.Result
	fails   map[string]error
	ran     []proc.Cmd
	bounded bool
	out     strings.Builder
	err     strings.Builder
}

func goodWorld() *world {
	return &world{
		dirs:    map[string]bool{repo: true, daemonDir: true},
		vars:    map[string]string{},
		answers: map[string]proc.Result{},
		fails:   map[string]error{},
	}
}

// key names a command by what it is: the program, and for the daemon's interpreter whether it is
// the probe or the run.
func key(c proc.Cmd) string {
	switch {
	case c.Argv[0] == daemonPy && slices.Contains(c.Argv, "-B"):
		return "run"
	case c.Argv[0] == daemonPy:
		return "probe"
	default:
		return c.Argv[0]
	}
}

func (w *world) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	w.ran = append(w.ran, c)
	_, w.bounded = ctx.Deadline()
	k := key(c)
	if err := w.fails[k]; err != nil {
		return proc.Result{}, err
	}
	if res, given := w.answers[k]; given {
		return res, nil
	}
	switch k {
	case resolver:
		return proc.Result{Stdout: daemonPy + "\n"}, nil
	case "probe":
		return proc.Result{Stdout: "3.11\n"}, nil
	case "python3.11":
		return proc.Result{Stdout: pytestPath + "\n"}, nil
	case "run":
		return proc.Result{Stdout: passOutput}, nil
	case "git":
		return proc.Result{Stdout: "/main/.git\n"}, nil
	}
	return proc.Result{Code: 127, Stderr: "no such program"}, nil
}

func (w *world) env() Env {
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     repo,
		Resolve: func(p string) (string, error) { return p, nil },
		Getenv:  func(k string) string { return w.vars[k] },
		IsDir:   func(p string) bool { return w.dirs[p] },
		Runner:  w,
	}
}

func (w *world) run(t *testing.T, want ExitCode, args ...string) {
	t.Helper()
	if got := Main(args, w.env()); got != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, w.out.String(), w.err.String())
	}
}

func (w *world) cmd(t *testing.T, k string) proc.Cmd {
	t.Helper()
	for _, c := range w.ran {
		if key(c) == k {
			return c
		}
	}
	t.Fatalf("%s never ran; ran %v", k, w.ran)
	return proc.Cmd{}
}

func mustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

// K7, K8: a provisioned machine runs the handler tests under the daemon's interpreter with the
// same-minor interpreter's pytest appended, and says how many passed.
func TestAProvisionedMachinePasses(t *testing.T) {
	w := goodWorld()
	w.run(t, ExitOK)
	if got := w.out.String(); got != "HOOK-HANDLERS-OK 101 test(s) under .claude/hooks/handlers\n" {
		t.Fatalf("stdout %q", got)
	}
	run := w.cmd(t, "run")
	want := []string{daemonPy, "-I", "-B", "-c", launcher, pytestPath, testsRoot, "-p", "no:cacheprovider", "-q", "--color=no"}
	if !slices.Equal(run.Argv, want) {
		t.Fatalf("argv %q\nwant %q", run.Argv, want)
	}
	if run.Dir != repo || !run.Merge {
		t.Fatalf("run in %q merged=%v", run.Dir, run.Merge)
	}
	if !slices.Contains(run.Env, "PYTEST_DISABLE_PLUGIN_AUTOLOAD=1") || !slices.Contains(run.Env, "PYTEST_ADDOPTS=") {
		t.Fatalf("env %q does not shut out plugins and caller options", run.Env)
	}
	if !w.bounded {
		t.Fatal("the run had no time limit")
	}
	mustContain(t, w.err.String(), "101 passed")
	if probe := w.cmd(t, "probe"); !slices.Equal(probe.Argv[:3], []string{daemonPy, "-I", "-c"}) {
		t.Fatalf("probe argv %q", probe.Argv)
	}
	if res := w.cmd(t, resolver); !slices.Equal(res.Argv, []string{resolver, "python", daemonDir}) {
		t.Fatalf("resolver argv %q", res.Argv)
	}
}

// K8: the skipped count is said, so a pass over skipped tests is visible.
func TestSkippedTestsAreCounted(t *testing.T) {
	w := goodWorld()
	w.answers["run"] = proc.Result{Stdout: "99 passed, 2 skipped in 0.2s\n"}
	w.run(t, ExitOK)
	mustContain(t, w.out.String(), "HOOK-HANDLERS-OK 99 test(s) under .claude/hooks/handlers, 2 skipped")
}

// K8: a failing test is exit 1.
func TestAFailingTestFails(t *testing.T) {
	w := goodWorld()
	w.answers["run"] = proc.Result{Code: 1, Stdout: "1 failed, 100 passed in 0.2s\n"}
	w.run(t, ExitFailed)
	mustContain(t, w.err.String(), "a handler test failed", "1 failed")
	if w.out.String() != "" {
		t.Fatalf("stdout %q", w.out.String())
	}
}

// K8: anything but a run to a verdict cannot pass.
func TestARunWithoutAVerdictCannotPass(t *testing.T) {
	cases := map[string]proc.Result{
		"no tests collected":     {Code: 5, Stdout: "no tests ran in 0.01s\n"},
		"usage error":            {Code: 4, Stdout: "ERROR: usage\n"},
		"passed with no count":   {Code: 0, Stdout: "all good\n"},
		"passed with zero tests": {Code: 0, Stdout: "0 passed in 0.01s\n"},
		"timed out":              {Code: -1, TimedOut: true},
		"leftover process":       {Code: 0, Stdout: passOutput, Leftover: true},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			w := goodWorld()
			w.answers["run"] = res
			w.run(t, ExitCannotRun)
			if w.out.String() != "" {
				t.Fatalf("stdout %q", w.out.String())
			}
		})
	}
}

// K7: a run that cannot start cannot pass.
func TestARunThatCannotStart(t *testing.T) {
	w := goodWorld()
	w.fails["run"] = errors.New("exec format error")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "exec format error")
}

// K3: the daemon comes from HOOKS_DAEMON_ROOT_DIR when it is set, and a set one that is not a
// directory is refused rather than looked for elsewhere.
func TestTheDaemonDirectoryFromTheEnvironment(t *testing.T) {
	w := goodWorld()
	w.vars["HOOKS_DAEMON_ROOT_DIR"] = "/elsewhere/daemon"
	w.dirs["/elsewhere/daemon"] = true
	w.answers["/elsewhere/daemon/scripts/lib/resolve_venv.sh"] = proc.Result{Stdout: daemonPy + "\n"}
	w.run(t, ExitOK)

	w = goodWorld()
	w.vars["HOOKS_DAEMON_ROOT_DIR"] = "/missing"
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "HOOKS_DAEMON_ROOT_DIR=/missing is not a directory")
}

// K3: a linked worktree has no daemon of its own and reads the main checkout's.
func TestALinkedWorktreeReadsTheMainCheckoutsDaemon(t *testing.T) {
	w := goodWorld()
	delete(w.dirs, daemonDir)
	w.dirs["/main/.claude/hooks-daemon"] = true
	w.answers["/main/.claude/hooks-daemon/scripts/lib/resolve_venv.sh"] = proc.Result{Stdout: daemonPy + "\n"}
	w.run(t, ExitOK)
	git := w.cmd(t, "git")
	if !slices.Equal(git.Argv, []string{"git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir"}) {
		t.Fatalf("git argv %q", git.Argv)
	}
}

// K3: no daemon anywhere is exit 2, naming the fix.
func TestNoDaemonNamesTheFix(t *testing.T) {
	w := goodWorld()
	delete(w.dirs, daemonDir)
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "no hooks daemon", "provision.sh")

	w = goodWorld()
	delete(w.dirs, daemonDir)
	w.answers["git"] = proc.Result{Code: 128, Stderr: "not a git repository"}
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "not a git repository")

	w = goodWorld()
	delete(w.dirs, daemonDir)
	w.fails["git"] = errors.New("git not found")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "git not found")
}

// K4: the daemon's interpreter is what its own resolver says; a resolver that fails, or answers
// with something that is not an absolute path, is exit 2.
func TestTheDaemonsInterpreterComesFromItsResolver(t *testing.T) {
	cases := map[string]proc.Result{
		"resolver fails":    {Code: 1, Stderr: "no venv"},
		"relative answer":   {Stdout: "bin/python\n"},
		"empty answer":      {Stdout: "\n"},
		"two-line answer":   {Stdout: daemonPy + "\n" + daemonPy + "\n"},
		"resolver timedout": {TimedOut: true, Code: -1},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			w := goodWorld()
			w.answers[resolver] = res
			w.run(t, ExitCannotRun)
			mustContain(t, w.err.String(), "provision.sh")
		})
	}
	w := goodWorld()
	w.fails[resolver] = errors.New("permission denied")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "permission denied")
}

// K5: an interpreter that cannot import the daemon, or does not say its version, is exit 2.
func TestTheDaemonsInterpreterMustImportTheDaemon(t *testing.T) {
	for name, res := range map[string]proc.Result{
		"import fails": {Code: 1, Stderr: "ModuleNotFoundError: claude_code_hooks_daemon"},
		"bad version":  {Stdout: "three\n"},
	} {
		t.Run(name, func(t *testing.T) {
			w := goodWorld()
			w.answers["probe"] = res
			w.run(t, ExitCannotRun)
			mustContain(t, w.err.String(), daemonPy)
		})
	}
	w := goodWorld()
	w.fails["probe"] = errors.New("no such file")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "no such file")
}

// K6: pytest is the one the interpreter of the daemon's minor version carries; none is exit 2
// naming where the project installs it.
func TestPytestComesFromTheSameMinorVersion(t *testing.T) {
	w := goodWorld()
	w.answers["probe"] = proc.Result{Stdout: "3.14\n"}
	w.answers["python3.14"] = proc.Result{Stdout: "/usr/lib/python3.14/site-packages\n"}
	w.run(t, ExitOK)
	if got := w.cmd(t, "run").Argv[5]; got != "/usr/lib/python3.14/site-packages" {
		t.Fatalf("pytest path %q", got)
	}

	for name, res := range map[string]proc.Result{
		"no pytest":       {Code: 1, Stderr: "ModuleNotFoundError: No module named 'pytest'"},
		"relative answer": {Stdout: "site-packages\n"},
	} {
		t.Run(name, func(t *testing.T) {
			sw := goodWorld()
			sw.answers["python3.11"] = res
			sw.run(t, ExitCannotRun)
			mustContain(t, sw.err.String(), "python3.11", "python3-pytest", "play-python.yml", ".claude/ccy/Dockerfile")
		})
	}
	w = goodWorld()
	w.fails["python3.11"] = errors.New("executable file not found")
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "executable file not found", "python3-pytest")
}

// K2: the CI workflow's exemption is honoured on GitHub Actions only, and says its reason.
func TestTheCIExemption(t *testing.T) {
	w := goodWorld()
	w.vars["QA_HOOK_HANDLERS_EXEMPT"] = "no daemon in CI"
	w.vars["GITHUB_ACTIONS"] = "true"
	w.run(t, ExitOK)
	if got := w.out.String(); got != "HOOK-HANDLERS-EXEMPT no daemon in CI\n" {
		t.Fatalf("stdout %q", got)
	}
	if len(w.ran) != 0 {
		t.Fatalf("an exempt run started %v", w.ran)
	}

	w = goodWorld()
	w.vars["QA_HOOK_HANDLERS_EXEMPT"] = "local shortcut"
	w.run(t, ExitCannotRun)
	mustContain(t, w.err.String(), "only on GitHub Actions")

	w = goodWorld()
	w.vars["GITHUB_ACTIONS"] = "true"
	w.run(t, ExitOK)
	mustContain(t, w.out.String(), "HOOK-HANDLERS-OK")
}

// K1: the shared root flags, help, and an unknown argument.
func TestTheCommandLine(t *testing.T) {
	w := goodWorld()
	w.run(t, ExitOK, "--help")
	mustContain(t, w.out.String(), "usage: qa-hook-handlers")

	w = goodWorld()
	w.run(t, ExitCannotRun, "--bogus")
	mustContain(t, w.err.String(), "unknown argument '--bogus'", "usage: qa-hook-handlers")

	w = goodWorld()
	w.run(t, ExitCannotRun, "--root")
	mustContain(t, w.err.String(), "--root needs a directory")

	w = goodWorld()
	w.dirs["/other"] = true
	w.dirs["/other/.claude/hooks-daemon"] = true
	w.answers["/other/.claude/hooks-daemon/scripts/lib/resolve_venv.sh"] = proc.Result{Stdout: daemonPy + "\n"}
	w.run(t, ExitOK, "--scripts", "/other/scripts")
	if dir := w.cmd(t, "run").Dir; dir != "/other" {
		t.Fatalf("ran in %q", dir)
	}
}

// K1: a root that cannot be resolved is exit 2.
func TestAnUnresolvableRoot(t *testing.T) {
	w := goodWorld()
	env := w.env()
	env.Resolve = func(string) (string, error) { return "", errors.New("no such directory") }
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
	mustContain(t, w.err.String(), "no such directory")
}

// An unwritable stdout cannot carry the verdict.
func TestAnUnwritableStdout(t *testing.T) {
	w := goodWorld()
	env := w.env()
	env.Stdout = failingWriter{}
	if got := Main(nil, env); got != ExitCannotRun {
		t.Fatalf("exit %d", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
