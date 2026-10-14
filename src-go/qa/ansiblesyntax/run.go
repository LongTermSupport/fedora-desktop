// Package ansiblesyntax is the Ansible syntax gate: every playbook the repository holds must pass
// `ansible-playbook --syntax-check`. Ansible is the engine; this package owns which files are
// playbooks, the proof that none was missed, the check that the machine can run the engine at
// all, and the judgement. The specification is the plan's SPEC-qa-ansible-syntax.md.
package ansiblesyntax

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// ExitCode is the status qa-ansible-syntax exits with.
type ExitCode int

// The statuses callers read: 0 every playbook parses, 1 at least one does not, 2 the run could
// not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultReportPath is where the machine report goes unless QA_JSON_OUT says otherwise.
const defaultReportPath = "/tmp/qa-ansible-syntax-results.json"

// The command-line flags that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Timeouts bound each process.
const (
	probeTimeout = time.Minute
	checkTimeout = 5 * time.Minute
)

// workers is how many playbooks are checked at once.
const workers = 4

// Env is everything the command touches outside the repository tree: the environment, the two
// streams, the working directory, the process seam and the report writer.
type Env struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git, ansible-config and ansible-playbook.
	Runner proc.Runner
	// WriteFile writes the report.
	WriteFile func(path string, data []byte) error
}

const usageText = `usage: qa-ansible-syntax [--root DIR] [--scripts DIR]

Runs ansible-playbook --syntax-check on every playbook in the repository: a YAML file git would
publish with a top-level "- hosts:" or "- import_playbook:" line. Parse only; no task runs.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

QA_JSON_OUT       where the machine report goes (default /tmp/qa-ansible-syntax-results.json).

Exit codes: 0 every playbook parses, 1 one does not, 2 bad usage, a missing prerequisite (the
engine, ansible.cfg, a password file the config names) or the run could not complete.
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

// absolute makes a path absolute against cwd and cleans it.
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
func (o options) scriptsDir(cwd, root string) string {
	if o.scripts != "" {
		return absolute(cwd, o.scripts)
	}
	return filepath.Join(root, "scripts")
}

// gate is one run.
type gate struct {
	env     Env
	root    string
	scripts string
	report  string
	// engineEnv pins the engine to the repository: its config, and plain text output.
	engineEnv []string
	stdout    bytes.Buffer
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
	g.scripts = opts.scriptsDir(env.Cwd, g.root)
	g.report = env.Getenv("QA_JSON_OUT")
	if g.report == "" {
		g.report = defaultReportPath
	}
	g.engineEnv = []string{
		"ANSIBLE_CONFIG=" + filepath.Join(g.root, "ansible.cfg"),
		// Ansible lets a force win over a no-colour, so every variable it reads for colour is pinned.
		"ANSIBLE_NOCOLOR=1", "NO_COLOR=1", "ANSIBLE_FORCE_COLOR=0",
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
	if err := g.requireMachine(ctx); err != nil {
		return ExitCannotRun, err
	}
	playbooks, err := g.discover()
	if err != nil {
		return ExitCannotRun, err
	}
	results, err := g.checkAll(ctx, playbooks)
	if err != nil {
		return ExitCannotRun, err
	}
	return g.finish(results)
}

// runTool runs one process with a time limit. A command that could not start or timed out is an
// error; one that ran and exited non-zero is a Result.
func (g *gate) runTool(ctx context.Context, limit time.Duration, argv ...string) (proc.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.root, Env: g.engineEnv})
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
