package changed

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qalock"
)

// ExitCode is the status qa-changed exits with.
type ExitCode int

// The three statuses callers read: 0 pass (or nothing to check), 1 a check failed, 2 could not
// run (bad usage, a missing tool, an unreadable input, or a check that could not run).
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// ExitInterrupted is the conventional status of a run ended by SIGINT.
const ExitInterrupted ExitCode = 130

// defaultTimeout is how long one check may run before it is killed. The longest suite and
// the Go gate finish well inside it; QA_CHANGED_TIMEOUT overrides it and 0 disables it.
const defaultTimeout = 30 * time.Minute

// Env is everything the command touches outside the repository: environment, streams, the
// process seam and the PATH lookup.
type Env struct {
	Getenv   func(string) string
	Stdout   io.Writer
	Stderr   io.Writer
	Runner   proc.Runner
	LookPath func(file string) error
	// Cwd is the working directory, the default root when neither --root nor QA_CHANGED_ROOT
	// is given and there is no scripts directory to derive it from.
	Cwd string
	// Lock takes the QA lock for the checks (qalock.OSTaker); a run without one is refused.
	Lock qalock.Taker
	// Umask sets the process's file mode creation mask and answers the old one
	// (syscall.Umask). Every command starts under gates.SuiteUmask; a run without one is
	// refused.
	Umask func(mask int) (old int)
}

const usageText = `usage: qa-changed [--base REF] [--plan] [--files FILE...] [--no-wait]

Diff-scoped QA for use while iterating, before each commit. Runs only the checks the CHANGED
files need; the full ./scripts/qa-all.bash (every gate, every suite) runs in GitHub CI on every
push, and CI's pass is what lets a protected branch move.

QA never runs in parallel: the checks run under the QA lock, shared by every worktree and
container of the checkout. While another QA run holds it this one waits, naming that run on
stderr once and then every minute. --plan, and a run with nothing changed, take no lock.

  (no flags)      files changed vs HEAD, plus untracked (not ignored)
  --base REF      files changed vs REF (e.g. origin/F44), so a burst of local commits is
                  covered, plus untracked
  --plan          print the chosen checks, run nothing
  --files FILE... use exactly these repo-relative files (no git diff); flags may follow
  --root DIR      the repository root (QA_CHANGED_ROOT does the same)
  --scripts DIR   the scripts directory holding the gates, suites and routing tables
  --no-wait       exit 2 at once, naming the holder, if another QA run holds the lock
  --only IDS      plan only the named checks (comma-separated check ids) and no suite
  --except IDS    plan every check but the named ones, and the suites; --only A and --except A
                  plan each check and suite once (CI splits a diff run across parallel jobs
                  this way). A name that is no check id is a usage error.
  -h, --help      this text

What each kind of file gets is scripts/qa-routing.tsv; the excluded trees are
scripts/qa-trees.tsv; the Go command suites are scripts/go-binary-suites.tsv. A changed file
that maps to no check is reported "not covered by qa-changed", never silently.

A local run is lean; CI (the CI variable set, not false or 0) runs everything as before:
  - a semgrep rule gate a changed file asks for scans the changed files only, unless the change
    touches the gate itself (its .semgrep rules, the QA module, its wrapper, the trees table);
  - a suite picked only because its Go command's build closure changed is skipped when the
    closure, src-go's go.mod and go.sum and the suite are what a recorded pass saw. Passes are
    kept under QA_SUITE_PASSES, else $XDG_CACHE_HOME or $HOME/.cache, in
    fedora-desktop-qa/suite-passes. CI=1 ./scripts/qa-changed.bash runs what CI runs.

QA_CHANGED_TIMEOUT is the per-check time limit as a Go duration (30m default, 0 disables).
QA_LOCK_TIMEOUT is the longest wait for the QA lock (default: as long as it is held).
QA_FULL_PRIORITY=1 runs at the priority started with; by default the run is nice 19, ionice idle.

Exit codes: 0 pass (or nothing changed), 1 a check failed, 2 bad usage, a missing tool, or a
check that could not run. stdout is the report; failure detail goes to stderr.
`

// options is the parsed command line.
type options struct {
	base     string
	planOnly bool
	noWait   bool
	help     bool
	explicit bool
	files    []string
	root     string
	scripts  string
	// picked is the check ids of --only or --except; except says which. Empty plans everything.
	picked []string
	except bool
}

// subset is whether the run plans only part of what the changed files need.
func (o *options) subset() bool { return len(o.picked) > 0 }

// subsetFlag is the flag the subset was given with, for messages.
func (o *options) subsetFlag() string {
	if o.except {
		return "--except"
	}
	return "--only"
}

// takeSubset reads the value of --only or --except: one subset per run, every name a check id.
func (o *options) takeSubset(rest []string, except bool, flag string) ([]string, error) {
	if o.subset() {
		return rest, &usageError{msg: "give one of --only and --except, once"}
	}
	var list string
	rest, err := o.value(rest, &list, flag+" needs a comma-separated list of check ids")
	if err != nil {
		return rest, err
	}
	names := strings.Split(list, ",")
	for _, name := range names {
		if !isCheckID(name) {
			return rest, &usageError{msg: fmt.Sprintf("%s names %q, which is no check id", flag, name)}
		}
	}
	o.picked, o.except = names, except
	return rest, nil
}

// usageError is a command line the tool refuses.
type usageError struct {
	msg string
	// listUsage is whether the usage text follows the message: it does for an argument the
	// tool does not know, not for a flag given a bad value.
	listUsage bool
}

func (e *usageError) Error() string { return e.msg }

// parseArgs reads the command line. Flags are recognised anywhere; the files are the arguments
// that are not flags, and "--" ends the flags. A value-taking flag refuses a following flag.
func parseArgs(args []string) (options, error) {
	opts := options{base: "HEAD"}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		if arg == "--" {
			if err := opts.takeFiles(rest); err != nil {
				return options{}, err
			}
			return opts, nil
		}
		var err error
		if rest, err = opts.apply(arg, rest); err != nil {
			return options{}, err
		}
	}
	if opts.explicit && len(opts.files) == 0 {
		return options{}, &usageError{msg: "--files needs at least one file"}
	}
	return opts, nil
}

// takeFiles is what follows "--": all files.
func (o *options) takeFiles(rest []string) error {
	if !o.explicit {
		if len(rest) > 0 {
			return &usageError{msg: fmt.Sprintf("unknown argument '%s'", rest[0]), listUsage: true}
		}
		return nil
	}
	o.files = append(o.files, rest...)
	if len(o.files) == 0 {
		return &usageError{msg: "--files needs at least one file"}
	}
	return nil
}

// apply handles one argument and returns the arguments left.
func (o *options) apply(arg string, rest []string) ([]string, error) {
	switch arg {
	case "--plan":
		o.planOnly = true
	case "--no-wait":
		o.noWait = true
	case "-h", "--help":
		o.help = true
	case "--files":
		o.explicit = true
	case "--base":
		return o.value(rest, &o.base, "--base needs a ref")
	case "--root":
		return o.value(rest, &o.root, "--root needs a directory")
	case "--scripts":
		return o.value(rest, &o.scripts, "--scripts needs a directory")
	case "--only", "--except":
		return o.takeSubset(rest, arg == "--except", arg)
	default:
		if !o.explicit || (len(arg) > 0 && arg[0] == '-') {
			return rest, &usageError{msg: fmt.Sprintf("unknown argument '%s'", arg), listUsage: true}
		}
		o.files = append(o.files, arg)
	}
	return rest, nil
}

// value takes the next argument as a flag's value; a missing one, or one that is itself a flag,
// is a usage error.
func (o *options) value(rest []string, into *string, missing string) ([]string, error) {
	if len(rest) == 0 || len(rest[0]) == 0 || rest[0][0] == '-' {
		return rest, &usageError{msg: missing}
	}
	*into = rest[0]
	return rest[1:], nil
}

// parseTimeout reads QA_CHANGED_TIMEOUT: empty is the default, "0" disables, otherwise a
// positive Go duration.
func parseTimeout(value string) (time.Duration, error) {
	if value == "" {
		return defaultTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, &usageError{msg: fmt.Sprintf("QA_CHANGED_TIMEOUT=%q is not a duration such as 30m (0 disables)", value)}
	}
	return d, nil
}
