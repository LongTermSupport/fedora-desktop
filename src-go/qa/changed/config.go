package changed

import (
	"fmt"
	"io"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
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
}

const usageText = `usage: qa-changed [--base REF] [--plan] [--files FILE...]

Diff-scoped QA for use while iterating. Runs only the checks the CHANGED files need; the full
./scripts/qa-all.bash (every gate, every suite) is needed only before a push.

  (no flags)      files changed vs HEAD, plus untracked (not ignored)
  --base REF      files changed vs REF (e.g. origin/F44), so a burst of local commits is
                  covered, plus untracked
  --plan          print the chosen checks, run nothing
  --files FILE... use exactly these repo-relative files (no git diff); flags may follow
  --root DIR      the repository root (QA_CHANGED_ROOT does the same)
  --scripts DIR   the scripts directory holding the gates, suites and routing tables
  -h, --help      this text

What each kind of file gets is scripts/qa-routing.tsv; the excluded trees are
scripts/qa-trees.tsv; the Go command suites are scripts/go-binary-suites.tsv. A changed file
that maps to no check is reported "not covered by qa-changed", never silently.

QA_CHANGED_TIMEOUT is the per-check time limit as a Go duration (30m default, 0 disables).

Exit codes: 0 pass (or nothing changed), 1 a check failed, 2 bad usage, a missing tool, or a
check that could not run. stdout is the report; failure detail goes to stderr.
`

// options is the parsed command line.
type options struct {
	base     string
	planOnly bool
	help     bool
	explicit bool
	files    []string
	root     string
	scripts  string
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
