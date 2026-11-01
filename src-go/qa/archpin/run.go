// Package archpin is the arch-pinned-artefact gate (Plan 00243, Defence Before Fix): the semgrep
// rule in .semgrep/arch-pin.yml, proven against its fixture, applied to every file git would
// publish, and judged against the ratchet scripts/arch-pin.allowlist. A download, package or
// image reference fixed to one CPU architecture installs nothing, or the wrong binary, on the
// other; the rule finds the literal, the allowlist holds the instances known when the rule
// landed, and a file may only lose them.
//
// semgrep is the engine (the rule is data); this package owns what is scanned (every regular file
// git would publish, minus the rule's own paths.exclude globs, handed by name), the proof the rule
// still matches its fixture, the refusal to pass over a file semgrep did not read, and the
// ratchet: a file not on the allowlist may have no finding; a listed file must have exactly its
// record (more is a new instance, fewer means the record is lowered, none or a file gone means
// the row is deleted).
//
// Exit: 0 clean, 1 a finding or a rule that fails its fixture, 2 the run could not complete.
package archpin

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

// ExitCode is the status qa-arch-pin exits with.
type ExitCode int

// The statuses callers read.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The rule, its fixture and its ratchet, relative to the repository root, and the rule's name.
const (
	ruleFile      = ".semgrep/arch-pin.yml"
	fixtureFile   = ".semgrep/arch-pin.fixture"
	allowlistFile = "scripts/arch-pin.allowlist"
	ruleName      = "arch-pinned-artefact"
)

// Timeouts bound each process.
const (
	versionTimeout = time.Minute
	gitTimeout     = 2 * time.Minute
	semgrepTimeout = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git and semgrep.
	Runner proc.Runner
}

const usageText = `usage: qa-arch-pin [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/arch-pin.yml (a download, package or image reference fixed
to one CPU architecture) to every file git would publish, after proving the rule against its
fixture, and judges the findings against the ratchet scripts/arch-pin.allowlist.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

// gate is one run.
type gate struct {
	env    Env
	root   string
	stdout bytes.Buffer
}

// Main runs the gate and returns the exit status.
func Main(args []string, env Env) ExitCode {
	parsed, err := rootargs.Strict(args)
	if err != nil {
		return fail(env, err, true)
	}
	if parsed.Help {
		return write(env, []byte(usageText))
	}
	root, err := rootpath.Root(env.Cwd, parsed.Root, parsed.Scripts)
	if err != nil {
		return fail(env, err, false)
	}
	g := &gate{env: env, root: root}
	verdict, err := g.run(context.Background())
	if err != nil {
		fail(env, err, false)
		return verdict
	}
	if code := write(env, g.stdout.Bytes()); code != ExitOK {
		return code
	}
	return verdict
}

// write sends text to stdout; the status is that of the write.
func write(env Env, text []byte) ExitCode {
	if _, err := env.Stdout.Write(text); err != nil {
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
func (g *gate) run(ctx context.Context) (ExitCode, error) {
	if err := g.requireSemgrep(ctx); err != nil {
		return ExitCannotRun, err
	}
	excl, err := g.readRule()
	if err != nil {
		return ExitCannotRun, err
	}
	list, err := g.readAllowlist()
	if err != nil {
		return ExitCannotRun, err
	}
	if code, proveErr := g.proveRule(ctx); proveErr != nil {
		return code, proveErr
	}
	files, err := g.handOver(ctx, excl)
	if err != nil {
		return ExitCannotRun, err
	}
	found, err := g.scan(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.verdict(list, found, files), nil
}

// The three bounds a process runs under; each is a constant, so no test can shorten one.
type bound int

const (
	versionBound bound = iota
	gitBound
	semgrepBound
)

// withBound is the context and the length of one bound.
func withBound(ctx context.Context, b bound) (context.Context, context.CancelFunc, time.Duration) {
	switch b {
	case versionBound:
		c, cancel := context.WithTimeout(ctx, versionTimeout)
		return c, cancel, versionTimeout
	case gitBound:
		c, cancel := context.WithTimeout(ctx, gitTimeout)
		return c, cancel, gitTimeout
	case semgrepBound:
	}
	c, cancel := context.WithTimeout(ctx, semgrepTimeout)
	return c, cancel, semgrepTimeout
}

// runTool runs one process under its bound. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, b bound, argv ...string) (proc.Result, error) {
	ctx, cancel, limit := withBound(ctx, b)
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
