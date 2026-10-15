// Package versionpins is the Plan 00109 gate on the upstream version-pin manifest,
// vars/version-pins.yml: the manifest must be well formed, and every row must still point at a
// file that exists and still declares the var that holds the pin. The manifest and the YAML
// targets are read with a YAML library; the `var: value` line form is the one the consumer
// (helpers/version_pins/check_pins.py) reads. The specification is the plan's
// SPEC-qa-version-pins.md.
package versionpins

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-version-pins exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 the manifest or a row is wrong, 2 the run could not
// judge.
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

const usageText = `usage: qa-version-pins [--root DIR] [--scripts DIR]

Plan 00109: vars/version-pins.yml is well formed, and every pin in it still names a file that
exists and still declares its var.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 the manifest or a pin is wrong, 2 bad usage or the run could not judge.
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
	return judge(check(env.Open(root)), out, errOut)
}

// coverage holds the number of pins that went through the on-disk check against the number the
// manifest declares. They differ only through a defect, and the counts printed are the ones counted.
func coverage(declared, reached int) error {
	if declared != reached {
		return fmt.Errorf("%d pin(s) declared but %d reached the on-disk check, so %d were never verified",
			declared, reached, declared-reached)
	}
	return nil
}

// judge reports a check: every problem and finding on stderr, the counted OK line on stdout.
func judge(r report, out, errOut *emitter) ExitCode {
	if len(r.problems) == 0 && len(r.findings) == 0 {
		if err := coverage(r.wellFormed, r.reached); err != nil {
			r.problems = append(r.problems, err.Error())
		}
	}
	for _, p := range r.problems {
		errOut.line("ERROR: " + p)
	}
	if n := len(r.findings); n > 0 {
		errOut.line(fmt.Sprintf("ERROR: %d version-pin finding(s) across %d pin(s):", n, r.pins))
		for _, f := range r.findings {
			errOut.line(fmt.Sprintf("  %s:%d: %s", manifestPath, f.line, f.text))
		}
	}
	switch {
	case len(r.problems) > 0:
		return ExitCannotRun
	case len(r.findings) > 0:
		return ExitFailed
	}
	out.line(fmt.Sprintf("%s: VERSION-PINS-OK %d pin(s), %d with install state tracked, %d declared untracked — "+
		"COVERAGE: %d of %d resolve to a live playbook var",
		manifestPath, r.pins, r.tracked, r.pins-r.tracked, r.reached, r.pins))
	return ExitOK
}
