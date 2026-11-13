// Package ghscopes is the gh-scope-outside-ssot gate: the semgrep rule in .semgrep/gh-scopes.yml,
// proven against its fixture, applied to every file git would publish, with a refusal to pass over
// any file the analyser did not read. semgrep is the engine (the rule is data); this package owns
// what is scanned, the proof the rule still works and the judgement of what semgrep reports. The
// specification is the plan's SPEC-qa-gh-scopes.md.
//
// The same engine, given its FindRace spec, is the find-race-negated-stat-test gate over
// .semgrep/find-race.yml, given its ArchPin spec the arch-pinned-artefact gate over
// .semgrep/arch-pin.yml, and given its ArgvLeak spec the secret-in-argv gate over
// .semgrep/argv-leak.yml, and given its PlaceIssuedToken spec the token-bundle-into-place-issued
// gate over .semgrep/place-issued-token.yml, and given its BootHold spec the
// unit-holds-boot-target gate over .semgrep/boot-hold.yml: one rule file, every published file,
// the same proofs.
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

// The gh-scopes rule and its fixture, relative to the repository root, and the rule's name in the
// output.
const (
	ruleFile    = ".semgrep/gh-scopes.yml"
	fixtureFile = ".semgrep/gh-scopes.fixture"
	ruleName    = "gh-scope-outside-ssot"
)

// Spec is one generic rule file and the gate that runs it over every published file: its short
// name (the heading of a skip warning), the rule file and fixture, the rule's identifier, what a
// finding line says the line does, and the usage text.
type Spec struct {
	short       string
	ruleFile    string
	fixtureFile string
	ruleName    string
	finding     string
	usage       string
	// stillScans names the rows of scripts/qa-trees.tsv this gate scans all the same. Only
	// `.semgrep`, for gh-scopes, find-race and arch-pin: they scanned the other rules and
	// fixtures before the table governed them, and narrowing a gate is the owner's call (Q18 of
	// issue #217, accept the narrowing or keep the coverage; pending). Each still drops its own
	// rule and fixture by its paths.exclude. TestOnlySemgrepIsStillScanned holds the list.
	stillScans []string
}

// semgrepTree is the rule and fixture tree of scripts/qa-trees.tsv, the one row a gate may still
// scan (Spec.stillScans).
const semgrepTree = ".semgrep"

// GHScopes is the gh-scope-outside-ssot gate.
var GHScopes = Spec{
	short:       "gh-scopes",
	ruleFile:    ruleFile,
	fixtureFile: fixtureFile,
	ruleName:    ruleName,
	finding:     "name a GitHub scope outside vars/github-required-scopes.yml",
	usage:       usageText,
	stillScans:  []string{semgrepTree},
}

// FindRace is the find-race-negated-stat-test gate (issue #152).
var FindRace = Spec{
	short:       "find-race",
	ruleFile:    ".semgrep/find-race.yml",
	fixtureFile: ".semgrep/find-race.fixture",
	ruleName:    "find-race-negated-stat-test",
	finding:     "run find -ignore_readdir_race with a negated stat test",
	usage:       findRaceUsageText,
	stillScans:  []string{semgrepTree},
}

// ArchPin is the arch-pinned-artefact gate (Plan 00243).
var ArchPin = Spec{
	short:       "arch-pin",
	ruleFile:    ".semgrep/arch-pin.yml",
	fixtureFile: ".semgrep/arch-pin.fixture",
	ruleName:    "arch-pinned-artefact",
	finding:     "fix a download, package or image to one CPU architecture",
	usage:       archPinUsageText,
	stillScans:  []string{semgrepTree},
}

// ArgvLeak is the secret-in-argv gate (issue #220). Its fixture is YAML so that the rule
// file's Ansible rule reads it too.
var ArgvLeak = Spec{
	short:       "secret-in-argv",
	ruleFile:    ".semgrep/argv-leak.yml",
	fixtureFile: ".semgrep/argv-leak.fixture.yml",
	ruleName:    "secret-in-argv",
	finding:     "put a secret on a command line",
	usage:       argvLeakUsageText,
}

// PlaceIssuedToken is the token-bundle-into-place-issued gate (issue #248).
var PlaceIssuedToken = Spec{
	short:       "place-issued-token",
	ruleFile:    ".semgrep/place-issued-token.yml",
	fixtureFile: ".semgrep/place-issued-token.fixture",
	ruleName:    "token-bundle-into-place-issued",
	finding:     "pipe a token bundle into seat place-issued",
	usage:       placeIssuedUsageText,
}

// BootHold is the unit-holds-boot-target gate (issue #263).
var BootHold = Spec{
	short:       "boot-hold",
	ruleFile:    ".semgrep/boot-hold.yml",
	fixtureFile: ".semgrep/boot-hold.fixture",
	ruleName:    "unit-holds-boot-target",
	finding:     "write a unit whose long start holds the boot target that wants it",
	usage:       bootHoldUsageText,
}

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

const findRaceUsageText = `usage: qa-find-race [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/find-race.yml (find -ignore_readdir_race with a negated
stat test, which matches an entry that vanished during the walk) to every file git would
publish, after proving the rule against its fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

const archPinUsageText = `usage: qa-arch-pin [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/arch-pin.yml (a download, package or image fixed to one
CPU architecture, with no selection by the architecture the build or host runs on) to every
file git would publish, after proving the rule against its fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

const argvLeakUsageText = `usage: qa-argv-leak [--root DIR] [--scripts DIR]

Applies the semgrep rules in .semgrep/argv-leak.yml (a passphrase, password or other
secret on a process's command line, or rendered by Ansible into one) to every file git would
publish, after proving the rules against their fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

const placeIssuedUsageText = `usage: qa-place-issued-token [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/place-issued-token.yml (a bundle issued with --credential
token piped into teamchat seat place-issued, which places only a pool seat's password) to every
file git would publish, after proving the rule against its fixture.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not complete.
`

const bootHoldUsageText = `usage: qa-boot-hold [--root DIR] [--scripts DIR]

Applies the semgrep rule in .semgrep/boot-hold.yml (a systemd unit wanted by a boot target
whose declared start is longer than 90 s and that is not ordered after that target, so the
target waits for it) to every file git would publish, after proving the rule against its
fixture.

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
	parsed, err := rootargs.Strict(args)
	return options{help: parsed.Help, root: parsed.Root, scripts: parsed.Scripts}, err
}

// gate is one run.
type gate struct {
	spec   Spec
	env    Env
	root   string
	stdout bytes.Buffer
}

// Main runs the gh-scopes gate and returns the exit status.
func Main(args []string, env Env) ExitCode { return GHScopes.Main(args, env) }

// Main runs the gate of this rule file and returns the exit status.
func (s Spec) Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		return s.fail(env, err, true)
	}
	if opts.help {
		return s.write(env, s.usage)
	}
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		return s.fail(env, err, false)
	}
	g := &gate{spec: s, env: env, root: root}
	verdict, err := g.run()
	if err != nil {
		s.fail(env, err, false)
		return verdict
	}
	if _, err := env.Stdout.Write(g.stdout.Bytes()); err != nil {
		return s.fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return verdict
}

// write sends text to stdout; the status is that of the write.
func (s Spec) write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return s.fail(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// fail prints an ERROR line (and the usage, for a usage error) on stderr. A stderr that cannot
// be written to has nowhere left to say so; the status is the same either way.
func (s Spec) fail(env Env, err error, usage bool) ExitCode {
	text := "ERROR: " + err.Error() + "\n"
	if usage {
		text += s.usage
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
	within, err := g.loadScope()
	if err != nil {
		return ExitCannotRun, err
	}
	if code, proveErr := g.proveRule(ctx); proveErr != nil {
		return code, proveErr
	}
	files, err := g.handOver(ctx, within)
	if err != nil {
		return ExitCannotRun, err
	}
	rep, err := g.scan(ctx, files)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.judge(files, within.rule, rep)
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
