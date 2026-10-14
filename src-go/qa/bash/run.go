// Package bash is the shell gate: every shell script git would publish must parse (bash -n) and
// have no shellcheck finding of level error or warning. bash and shellcheck are the engines; this
// package owns what is checked, the proof that no tracked script was left out, how the engines are
// invoked (so no file beside a script can change the rules), and the judgement. The specification
// is the plan's SPEC-qa-bash.md.
package bash

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the status qa-bash exits with.
type ExitCode int

// The statuses callers read: 0 no finding, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The report paths unless the environment says otherwise.
const (
	defaultReportPath = "/tmp/qa-bash-results.json"
	defaultMergedPath = "/tmp/qa-results.json"
)

// The command-line flags that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// flagDirectives takes every argument after it as a file.
const flagDirectives = "--directives"

// Timeouts bound each process.
const (
	probeTimeout  = time.Minute
	syntaxTimeout = time.Minute
	lintTimeout   = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam and the report writer.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git, bash and shellcheck.
	Runner proc.Runner
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-bash [--root DIR] [--scripts DIR] [--directives FILE...]

Checks every shell script git would publish (tracked, or untracked and not ignored): it must
parse (bash -n), have no shellcheck finding of level error or warning, and hide none behind a
shellcheck directive that scripts/shellcheck-disable.allowlist does not record.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  --directives    only the directive check, over the files that follow (root-relative)
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-bash-results.json).
QA_RESULTS_JSON   the merged qa-all report the advisory line names (default /tmp/qa-results.json).

Exit codes: 0 no finding, 1 a finding, 2 bad usage or the run could not complete.
stdout is the findings and the verdict; failure to run goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
	// directives is --directives: only the suppression scan, over files (SPEC-qa-bash.md S20).
	directives bool
	files      []string
}

// parseArgs reads the command line; anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			opts.help = true
		case flagDirectives:
			opts.directives, opts.files = true, args[i+1:]
			if len(opts.files) == 0 {
				return opts, fmt.Errorf("%s needs at least one file", flagDirectives)
			}
			return opts, nil
		case flagRoot, flagScripts:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == flagRoot {
				opts.root = args[i]
			} else {
				opts.scripts = args[i]
			}
		default:
			return opts, fmt.Errorf("unknown argument '%s'", arg)
		}
	}
	return opts, nil
}

// absolute makes a path absolute against cwd and cleans it, so a relative path or a trailing
// slash cannot make the paths the tools print differ from the ones they were given.
func absolute(cwd, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// rootDir resolves the repository root.
func (o options) rootDir(cwd string) string {
	switch {
	case o.root != "":
		return absolute(cwd, o.root)
	case o.scripts != "":
		return filepath.Dir(absolute(cwd, o.scripts))
	}
	return absolute(cwd, cwd)
}

// scriptsDir resolves the scripts directory under a root.
func (o options) scriptsDir(root string) string {
	if o.scripts != "" {
		return absolute(root, o.scripts)
	}
	return filepath.Join(root, "scripts")
}

// gate is one run.
type gate struct {
	env    Env
	root   string
	script string
	report string
	merged string
	stdout bytes.Buffer
}

// Main runs the gate and returns the exit status.
func Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		return fail(env, err, true)
	}
	if opts.help {
		return write(env, usageText)
	}
	g := &gate{env: env, root: opts.rootDir(env.Cwd)}
	g.script = opts.scriptsDir(g.root)
	g.report = orDefault(env.Getenv("QA_JSON_OUT"), defaultReportPath)
	g.merged = orDefault(env.Getenv("QA_RESULTS_JSON"), defaultMergedPath)
	var verdict ExitCode
	if opts.directives {
		verdict, err = g.runDirectives(context.Background(), opts.files)
	} else {
		verdict, err = g.run()
	}
	if err != nil {
		return fail(env, err, false)
	}
	if _, err := env.Stdout.Write(g.stdout.Bytes()); err != nil {
		return fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return verdict
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// write sends text to stdout; the status is that of the write.
func write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// fail prints an ERROR line (and the usage, for a usage error) on stderr. A stderr that cannot
// be written to has nowhere left to say so; the status is the same either way.
func fail(env Env, err error, usage bool) ExitCode {
	text := "ERROR: " + err.Error() + "\n"
	if usage {
		text += usageText
	}
	return statusAfter(io.WriteString(env.Stderr, text))
}

// statusAfter is the status of a run that could not complete, whether or not its explanation
// could be written.
func statusAfter(int, error) ExitCode { return ExitCannotRun }

// run is the whole check; every error is a run that could not complete.
func (g *gate) run() (ExitCode, error) {
	ctx := context.Background()
	if err := g.requireTools(ctx); err != nil {
		return ExitCannotRun, err
	}
	files, err := g.discover()
	if err != nil {
		return ExitCannotRun, err
	}
	hidden, err := g.directiveFindings(ctx, files, true)
	if err != nil {
		return ExitCannotRun, err
	}
	syntax, err := g.syntax(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	diagnostics, err := g.shellcheck(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.finish(files, syntax, diagnostics, hidden)
}

// runTool runs one process with a time limit. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, limit time.Duration, cmd proc.Cmd) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd.Dir = g.root
	res, err := g.env.Runner.Run(ctx, cmd)
	if err != nil {
		return res, fmt.Errorf("%s: %w", cmd.Argv[0], err)
	}
	if res.TimedOut {
		return res, fmt.Errorf("%s did not finish within %s", cmd.Argv[0], limit)
	}
	if res.Leftover {
		return res, fmt.Errorf("%s left a process holding its output open", cmd.Argv[0])
	}
	return res, nil
}
