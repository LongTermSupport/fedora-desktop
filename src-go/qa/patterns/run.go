// Package patterns is the pattern gate for repository-owned shell scripts: it applies the
// semgrep rules in .semgrep/bash-conventions.yml to every shell script git would publish, and
// refuses a pass over any code the analyser did not read. semgrep is the engine that matches the
// rules (they are data); this package owns what is scanned, the proof that the rules still work,
// and the judgement of what semgrep reports. The specification is the plan's SPEC-qa-patterns.md.
package patterns

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-patterns exits with.
type ExitCode int

// The statuses callers read: 0 no finding, 1 at least one, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-patterns-results.json"

// Where the rules live, relative to the repository root, and the file that names the trees the
// gates must not gate on (relative to the scripts directory).
const (
	rulesPath   = ".semgrep/bash-conventions.yml"
	fixturePath = ".semgrep/bash-conventions.bash"
	treesFile   = "qa-trees.tsv"
)

// Timeouts bound each process.
const (
	gitTimeout     = 2 * time.Minute
	semgrepTimeout = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam and the report writer.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git and semgrep.
	Runner proc.Runner
	// TempDir is the directory the scan mirror is made under ("" is the system's).
	TempDir string
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-patterns [--root DIR] [--scripts DIR]

Applies the semgrep rules in .semgrep/bash-conventions.yml to every shell script git would
publish (tracked, or untracked and not ignored), after proving the rules against their fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-patterns-results.json).

Exit codes: 0 no finding, 1 a finding, 2 bad usage or the run could not complete.
stdout is the coverage notes and the verdict; failure to run goes to stderr.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// parseArgs reads the command line; anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	parsed, err := rootargs.Strict(args)
	return options{help: parsed.Help, root: parsed.Root, scripts: parsed.Scripts}, err
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
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		return fail(env, err, false)
	}
	g := &gate{env: env, root: root}
	g.scripts = opts.scriptsDir(g.root)
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
	if err := g.requireSemgrep(ctx); err != nil {
		return ExitCannotRun, err
	}
	if err := g.proveRules(ctx); err != nil {
		return ExitCannotRun, err
	}
	scripts, links, err := g.discover(ctx)
	if err != nil {
		return ExitCannotRun, err
	}
	if links.covered > 0 {
		fmt.Fprintf(&g.stdout, "⚠ patterns: %d symlink(s) not scanned in their own right (their targets are, under their own names)\n", links.covered)
	}
	if len(links.own) > 0 {
		fmt.Fprintf(&g.stdout, "⚠ patterns: %d symlink(s) scanned under their own name, their targets being outside the scanned set:\n%s\n", len(links.own), indent(links.own))
	}
	scan, err := g.scan(ctx, scripts)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.finish(scripts, scan)
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
