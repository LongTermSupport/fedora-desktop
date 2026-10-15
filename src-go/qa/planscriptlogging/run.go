// Package planscriptlogging is the Plan 00130 gate: an active plan script must not send its own
// output into a process substitution (`exec > >(tee "$LOG") 2>&1`), because the shell cannot wait
// on one and the run log loses its last chunk; and no active plan folder may keep a local `logs/`
// directory, which hides unscrubbed run logs from `git status`. The scripts are judged on the
// bash syntax tree. The specification is the plan's SPEC-qa-plan-script-logging.md.
package planscriptlogging

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-plan-script-logging exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 an offence found, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks; a test on a fake tree gives
	// its own).
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
}

const usageText = `usage: qa-plan-script-logging [--root DIR] [--scripts DIR]

Plan 00130: no active plan script (CLAUDE/Plan/NNNNN-*/) may send its own output to a process
substitution ('exec > >(tee "$LOG")'), and no active plan folder may keep a logs/ directory.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 an offence, 2 bad usage or the run could not complete.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// unknownArgError is an argument the tool does not know; the usage text follows it.
type unknownArgError struct{ arg string }

func (e *unknownArgError) Error() string { return fmt.Sprintf("unknown argument '%s'", e.arg) }

func parseArgs(args []string) (options, error) {
	var opts options
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "-h", "--help":
			opts.help = true
		case "--root", "--scripts":
			if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
				return options{}, fmt.Errorf("%s needs a directory", arg)
			}
			if arg == "--root" {
				opts.root = rest[0]
			} else {
				opts.scripts = rest[0]
			}
			rest = rest[1:]
		default:
			return options{}, &unknownArgError{arg: arg}
		}
	}
	return opts, nil
}

// emitter writes to a stream and remembers the first write that failed.
type emitter struct {
	w   io.Writer
	err error
}

func (e *emitter) line(text string) {
	if _, err := fmt.Fprintln(e.w, text); err != nil && e.err == nil {
		e.err = err
	}
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := &emitter{w: env.Stdout}, &emitter{w: env.Stderr}
	code := run(args, env, out, errOut)
	if out.err != nil {
		errOut.line("ERROR: cannot write to stdout: " + out.err.Error())
		return ExitCannotRun
	}
	if code != ExitCannotRun && errOut.err != nil {
		return ExitCannotRun
	}
	return code
}

func run(args []string, env Env, out, errOut *emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*unknownArgError](err); isUnknown && unknown != nil {
			for line := range strings.SplitSeq(strings.TrimSuffix(usageText, "\n"), "\n") {
				errOut.line(line)
			}
		}
		return ExitCannotRun
	}
	if opts.help {
		out.line(strings.TrimSuffix(usageText, "\n"))
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.root, opts.scripts)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return judge(scan(env.Open(root)), out, errOut)
}

// judge reports a scan: every problem and finding on stderr, the counted OK line on stdout.
func judge(r result, out, errOut *emitter) ExitCode {
	if len(r.problems) == 0 && r.examined == 0 {
		r.problems = append(r.problems, "examined 0 plan scripts; the search matched nothing, so this gate certified nothing")
	}
	for _, p := range r.problems {
		errOut.line("ERROR: " + p)
	}
	if n := len(r.findings); n > 0 {
		errOut.line(fmt.Sprintf("ERROR: %d plan-script logging offence(s) across %d script(s) examined:", n, r.examined))
		for _, f := range r.findings {
			errOut.line("  " + f)
		}
		errOut.line("")
		errOut.line("  Convert to the plan library: source _planlib.inc.bash, plan_init, then")
		errOut.line("  plan_start_log auto — and remove every consumer of the $LOG you delete.")
	}
	switch {
	case len(r.problems) > 0:
		return ExitCannotRun
	case len(r.findings) > 0:
		return ExitFailed
	}
	out.line(fmt.Sprintf("PLAN-SCRIPT-LOGGING-OK: %d plan script(s) examined, no offences", r.examined))
	return ExitOK
}
