// Package js is the JavaScript gate: every JavaScript file git would publish must parse
// (node --check), and the GNOME-extension project must pass ESLint under the repository's own
// config. node and ESLint are the engines; this package owns what is checked, the proof that
// ESLint read every file it was given, the pins that keep anything but the repository's config
// from changing the rules, and the judgement. The specification is the plan's SPEC-qa-js.md.
package js

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-js exits with.
type ExitCode int

// The statuses callers read: 0 no finding, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-js-results.json"

// The command-line flags that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Timeouts bound each process.
const (
	probeTimeout = time.Minute
	nodeTimeout  = 5 * time.Minute
	eslintLimit  = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam and the report writer.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git, node and ESLint.
	Runner proc.Runner
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-js [--root DIR] [--scripts DIR]

Parse-checks (node --check) every .js and .mjs file git would publish, and lints the ones under
extensions/ with the project's ESLint under the repository's config only (no config lookup, no
inline directives).

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-js-results.json).

Exit codes: 0 no finding, 1 a finding, 2 bad usage or the run could not complete.
stdout is the findings and the verdict; failure to run goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// parseArgs reads the command line; anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			opts.help = true
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

// absolute makes a path absolute against cwd and cleans it, so the paths handed to node and
// ESLint and the ones they print are the same text.
func absolute(cwd, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// scriptsDir resolves the scripts directory under a root.
func (o options) scriptsDir(root string) string {
	if o.scripts != "" {
		return o.scripts
	}
	return filepath.Join(root, "scripts")
}

// gate is one run.
type gate struct {
	env     Env
	root    string
	scripts string
	report  string
	stdout  bytes.Buffer
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
	g := &gate{env: env}
	if g.root, err = rootpath.Root(env.Cwd, opts.root, opts.scripts); err != nil {
		return fail(env, err, false)
	}
	g.scripts = absolute(env.Cwd, opts.scriptsDir(g.root))
	g.report = env.Getenv("QA_JSON_OUT")
	if g.report == "" {
		g.report = defaultReportPath
	}
	verdict, err := g.run()
	if err != nil {
		return fail(env, err, false)
	}
	if _, err := env.Stdout.Write(g.stdout.Bytes()); err != nil {
		return fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return verdict
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
	lintable := g.lintable(files)
	if len(lintable) == 0 {
		return ExitCannotRun, fmt.Errorf("no JavaScript file under %s/ to lint: a lint over nothing must not read as a pass", extensionsDir)
	}
	parse, err := g.parseCheck(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	findings, err := g.lint(ctx, lintable)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.finish(files, lintable, parse, findings)
}

// runTool runs one process with a time limit. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, limit time.Duration, argv ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.root})
	if err != nil {
		return res, fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
	}
	if res.TimedOut {
		return res, fmt.Errorf("%s did not finish within %s", filepath.Base(argv[0]), limit)
	}
	if res.Leftover {
		return res, fmt.Errorf("%s left a process holding its output open", filepath.Base(argv[0]))
	}
	return res, nil
}
