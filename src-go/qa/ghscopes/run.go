// Package ghscopes is the gh-scope-outside-ssot gate: the semgrep rule in .semgrep/gh-scopes.yml,
// proven against its fixture, applied to every file git would publish, with a refusal to pass over
// any file the analyser did not read. semgrep is the engine (the rule is data); this package owns
// what is scanned, the proof the rule still works and the judgement of what semgrep reports. The
// specification is the plan's SPEC-qa-gh-scopes.md.
package ghscopes

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

// ExitCode is the status qa-gh-scopes exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a finding or a rule that fails its fixture, 2 the run
// could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The rule and its fixture, relative to the repository root, and the rule's name in the output.
const (
	ruleFile    = ".semgrep/gh-scopes.yml"
	fixtureFile = ".semgrep/gh-scopes.fixture"
	ruleName    = "gh-scope-outside-ssot"
)

// Timeouts bound each process.
const (
	versionTimeout = time.Minute
	gitTimeout     = 2 * time.Minute
	semgrepTimeout = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree: the two streams, the
// working directory and the process seam.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git and semgrep.
	Runner proc.Runner
}

const usageText = `usage: qa-gh-scopes [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/gh-scopes.yml (a GitHub OAuth scope named outside
vars/github-required-scopes.yml) to every file git would publish, after proving the rule against
its fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
}

// parseArgs reads the command line; anything unknown is a usage error.
func parseArgs(args []string) (options, error) {
	parsed, err := rootargs.Lenient(args)
	return options{help: parsed.Help, root: parsed.Root, scripts: parsed.Scripts}, err
}

// gate is one run.
type gate struct {
	env    Env
	root   string
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
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		return fail(env, err, false)
	}
	g := &gate{env: env, root: root}
	verdict, err := g.run()
	if err != nil {
		fail(env, err, false)
		return verdict
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

// path returns the absolute path of a file under the root.
func (g *gate) path(rel string) string { return filepath.Join(g.root, filepath.FromSlash(rel)) }

// run is the whole check; an error is a run that stopped, with the status it stops at.
func (g *gate) run() (ExitCode, error) {
	ctx := context.Background()
	if err := g.requireSemgrep(ctx); err != nil {
		return ExitCannotRun, err
	}
	dropped, err := g.readRule()
	if err != nil {
		return ExitCannotRun, err
	}
	if code, proveErr := g.proveRule(ctx); proveErr != nil {
		return code, proveErr
	}
	files, err := g.handOver(ctx)
	if err != nil {
		return ExitCannotRun, err
	}
	rep, err := g.scan(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.judge(files, dropped, rep)
}

// runTool runs one process with a time limit. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, limit time.Duration, argv ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.root})
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
