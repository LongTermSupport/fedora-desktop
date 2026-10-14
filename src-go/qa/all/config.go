// Package all is qa-all: the full QA run, every gate of scripts/qa-gates.tsv in order, never
// stopping at the first failure, with one stage line per gate, a machine report and one verdict.
// It is the gate before a push. SPEC-qa-all.md is its specification.
package all

import (
	"fmt"
	"io"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
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
}

const usageText = `usage: qa-all [--root DIR] [--scripts DIR]

The full QA run, the gate before a push. Runs every gate of scripts/qa-gates.tsv in order,
never stopping at the first failure, prints one stage line per gate and one verdict, and
writes the machine report (jq '.failures[]' on it names every failing file).

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory holding qa-gates.tsv and the gates
  -h, --help      this text

QA_ALL_TIMEOUT    the per-gate time limit as a Go duration (30m default, 0 disables).
QA_RESULTS_JSON   where the machine report goes (default /tmp/qa-results.json).

Exit codes: 0 every gate passed, 1 a gate failed, 2 bad usage or the run could not complete
(a stage reporting a missing tool, an unreadable gate table). stdout is the stage lines and the
verdict; failure detail goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
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
		case "--root":
			rest, err = takeValue(rest, &opts.root, "--root needs a directory")
		case "--scripts":
			rest, err = takeValue(rest, &opts.scripts, "--scripts needs a directory")
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
