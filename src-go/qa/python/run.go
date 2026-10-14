// Package python is the Python gate: every Python file git would publish must compile and pass
// ruff, and every Python template must compile once its expressions are substituted. ruff is the
// engine that lints (the ruleset is data in ruff.toml, the version is pinned by .qa-versions) and
// python3 the compiler; this package owns what is checked, the proof that ruff read what it was
// given, and the judgement. The specification is the plan's SPEC-qa-python.md.
package python

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the status qa-python exits with.
type ExitCode int

// The statuses callers read: 0 no finding, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-python-results.json"

// Where the pin and the excluded trees live, relative to the repository root and the scripts
// directory.
const (
	pinFile   = ".qa-versions"
	treesFile = "qa-trees.tsv"
)

// Timeouts bound each process.
const (
	probeTimeout   = time.Minute
	compileTimeout = 10 * time.Minute
	ruffTimeout    = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam and the report writer.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git, ruff and python3.
	Runner proc.Runner
	// TempDir is the directory the rendered templates are written under ("" is the system's).
	TempDir string
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-python [--root DIR] [--scripts DIR]

Compiles and lints (ruff) every Python file git would publish (tracked, or untracked and not
ignored), and compiles every Python template with its {{ }} expressions substituted.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-python-results.json).

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
		case "--root", "--scripts":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == "--root" {
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

// rootDir resolves the repository root.
func (o options) rootDir(cwd string) string {
	switch {
	case o.root != "":
		return o.root
	case o.scripts != "":
		return filepath.Dir(filepath.Clean(o.scripts))
	}
	return cwd
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
	stderr  bytes.Buffer
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
	g.root = opts.rootDir(env.Cwd)
	g.scripts = opts.scriptsDir(g.root)
	g.report = env.Getenv("QA_JSON_OUT")
	if g.report == "" {
		g.report = defaultReportPath
	}
	verdict, err := g.run()
	if err != nil {
		return fail(env, err, false)
	}
	if _, err := env.Stderr.Write(g.stderr.Bytes()); err != nil {
		return fail(env, fmt.Errorf("cannot write to stderr: %w", err), false)
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
	ruffVersion, err := g.requireTools(ctx)
	if err != nil {
		return ExitCannotRun, err
	}
	pin, err := g.readPin()
	if err != nil {
		return ExitCannotRun, err
	}
	if ruffVersion != pin {
		return g.versionMismatch(pin, ruffVersion)
	}
	src, err := g.discover(ctx)
	if err != nil {
		return ExitCannotRun, err
	}
	cleanup, rendered, err := g.render(src.templates)
	verdict := ExitCannotRun
	if err == nil {
		verdict, err = g.check(ctx, src, rendered)
	}
	if cleanupErr := cleanup(); cleanupErr != nil && err == nil {
		return ExitCannotRun, cleanupErr
	}
	return verdict, err
}

// check compiles and lints what was found and judges the result.
func (g *gate) check(ctx context.Context, src sources, rendered []renderedTemplate) (ExitCode, error) {
	compiled, err := g.compile(ctx, src.files, rendered)
	if err != nil {
		return ExitCannotRun, err
	}
	diagnostics, err := g.ruff(ctx, src.files)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.finish(src, rendered, compiled, diagnostics)
}

// runTool runs one process with a time limit. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, limit time.Duration, dir string, argv ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: dir})
	if err != nil {
		return res, fmt.Errorf("%s: %w", argv[0], err)
	}
	if res.TimedOut {
		return res, fmt.Errorf("%s did not finish within %s", argv[0], limit)
	}
	if res.Leftover {
		return res, fmt.Errorf("%s left a process holding its output open", argv[0])
	}
	return res, nil
}
