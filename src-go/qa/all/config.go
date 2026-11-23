// Package all is qa-all: the full QA run, every gate of scripts/qa-gates.tsv in order, never
// stopping at the first failure, with one stage line per gate, a machine report and one verdict.
// GitHub CI runs it on every push, and its pass there is what lets a protected branch move
// (qa/pushgate). SPEC-qa-all.md is its specification.
package all

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qalock"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
)

// ExitCode is the status qa-all exits with.
type ExitCode int

// The statuses callers read: 0 every gate passed, 1 at least one failed, 2 the run could not
// complete (bad usage, an unreadable gate table, a stage that reports a missing tool).
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// ExitInterrupted is the conventional status of a run ended by SIGINT.
const ExitInterrupted ExitCode = 130

// defaultTimeout is how long one gate may run before it is killed. QA_ALL_TIMEOUT overrides it
// and 0 disables it.
const defaultTimeout = 30 * time.Minute

// defaultReportPath is where the machine report goes; CI uploads this path.
const defaultReportPath = "/tmp/qa-results.json"

// Env is everything the command touches outside the repository: environment, streams, the
// process seam, and the source of the helper suite's counts-file token.
type Env struct {
	Getenv   func(string) string
	Stdout   io.Writer
	Stderr   io.Writer
	Runner   proc.Runner
	NewToken func() string
	// Cwd is the working directory, the default root when neither --root nor --scripts is given.
	Cwd string
	// Lock takes the QA lock for the run (qalock.OSTaker); a run without one is refused.
	Lock qalock.Taker
	// Umask sets the process's file mode creation mask and answers the old one
	// (syscall.Umask). Every command starts under gates.SuiteUmask; a run without one is
	// refused.
	Umask func(mask int) (old int)
	// Locate names the commit the run judges and where its verdict goes (qaverdict.Locate over
	// the process seam); a run without one is refused.
	Locate func(ctx context.Context, root string) (qaverdict.Target, error)
	// Now is the clock the verdict's start and end are read from.
	Now func() time.Time
}

const usageText = `usage: qa-all [--root DIR] [--scripts DIR] [--no-wait] [--only GATES | --except GATES]

The full QA run, which GitHub CI runs on every push. Runs every gate of scripts/qa-gates.tsv
in order, never stopping at the first failure, prints one stage line per gate and one
verdict, and writes the machine report (jq '.failures[]' on it names every failing file).

QA never runs in parallel: the run holds the QA lock, shared by every worktree and container
of the checkout, from its first gate to its verdict. While another QA run holds it this one
waits, naming that run on stderr once and then every minute.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory holding qa-gates.tsv and the gates
  --no-wait       exit 2 at once, naming the holder, if another QA run holds the lock
  --only GATES    run only the named gates (comma-separated stage-line names), in table order
  --except GATES  run every gate but the named ones; --only A and --except A run each gate once
                  (CI splits the table across parallel jobs this way). A name the table does not
                  hold is exit 2. A subset is not the full run: it writes no verdict record.
  -h, --help      this text

QA_ALL_TIMEOUT    the per-gate time limit as a Go duration (30m default, 0 disables).
QA_RESULTS_JSON   where the machine report goes (default /tmp/qa-results.json).
QA_LOCK_TIMEOUT   the longest wait for the QA lock as a Go duration (default: as long as it is held).
QA_FULL_PRIORITY  1 runs at the priority started with; by default the run is nice 19, ionice idle.

Exit codes: 0 every gate passed, 1 a gate failed, 2 bad usage or the run could not complete
(a stage reporting a missing tool, an unreadable gate table). stdout is the stage lines and the
verdict; failure detail goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	noWait  bool
	root    string
	scripts string
	// picked is the gate names of --only or --except; except says which. Empty is every gate.
	picked []string
	except bool
}

// subset is whether the run takes only part of the table.
func (o options) subset() bool { return len(o.picked) > 0 }

// subsetFlag is the flag the subset was given with, for messages.
func (o options) subsetFlag() string {
	if o.except {
		return "--except"
	}
	return "--only"
}

// takeSubset reads the value of --only or --except: one subset per run, every name non-empty.
func takeSubset(rest []string, opts *options, except bool, flag string) ([]string, error) {
	if opts.subset() {
		return rest, &usageError{msg: "give one of --only and --except, once"}
	}
	var list string
	rest, err := takeValue(rest, &list, flag+" needs a comma-separated list of gate names")
	if err != nil {
		return rest, err
	}
	names := strings.Split(list, ",")
	if slices.Contains(names, "") {
		return rest, &usageError{msg: fmt.Sprintf("%s %q holds an empty gate name", flag, list)}
	}
	opts.picked, opts.except = names, except
	return rest, nil
}

// pickGates is the gates of the table a subset runs, in table order. A name the table does not
// hold is refused, so a renamed gate cannot drop out of every CI job unnoticed; a subset that
// leaves nothing to run proves nothing and is refused too.
func pickGates(table []gates.Gate, opts options) ([]gates.Gate, error) {
	if !opts.subset() {
		return table, nil
	}
	for _, name := range opts.picked {
		if !slices.ContainsFunc(table, func(g gates.Gate) bool { return g.Name == name }) {
			return nil, fmt.Errorf("%s names %q, which is no gate of qa-gates.tsv", opts.subsetFlag(), name)
		}
	}
	var picked []gates.Gate
	for _, g := range table {
		if slices.Contains(opts.picked, g.Name) != opts.except {
			picked = append(picked, g)
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("%s %s leaves no gate to run", opts.subsetFlag(), strings.Join(opts.picked, ","))
	}
	return picked, nil
}

// usageError is a command line the tool refuses.
type usageError struct {
	msg string
	// listUsage is whether the usage text follows the message: it does for an argument the tool
	// does not know, not for a flag given a bad value.
	listUsage bool
}

func (e *usageError) Error() string { return e.msg }

// parseArgs reads the command line. The bash ignored every argument and ran the whole suite, so
// a typo cost the minutes of a full run; here anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		var err error
		switch arg {
		case "-h", "--help":
			opts.help = true
		case "--no-wait":
			opts.noWait = true
		case "--root":
			rest, err = takeValue(rest, &opts.root, "--root needs a directory")
		case "--scripts":
			rest, err = takeValue(rest, &opts.scripts, "--scripts needs a directory")
		case "--only", "--except":
			rest, err = takeSubset(rest, &opts, arg == "--except", arg)
		default:
			err = &usageError{msg: fmt.Sprintf("unknown argument '%s'", arg), listUsage: true}
		}
		if err != nil {
			return options{}, err
		}
	}
	return opts, nil
}

// takeValue takes the next argument as a flag's value; a missing one, or one that is itself a
// flag, is a usage error.
func takeValue(rest []string, into *string, missing string) ([]string, error) {
	if len(rest) == 0 || len(rest[0]) == 0 || rest[0][0] == '-' {
		return rest, &usageError{msg: missing}
	}
	*into = rest[0]
	return rest[1:], nil
}

// parseTimeout reads QA_ALL_TIMEOUT: empty is the default, "0" disables, otherwise a positive
// Go duration.
func parseTimeout(value string) (time.Duration, error) {
	if value == "" {
		return defaultTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, &usageError{msg: fmt.Sprintf("QA_ALL_TIMEOUT=%q is not a duration such as 30m (0 disables)", value)}
	}
	return d, nil
}
