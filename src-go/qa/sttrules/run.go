// Package sttrules is the gate for the rules in .semgrep/speech-to-text.yml: a model judged
// present from its cache snapshot, and a dropdown label that explains itself. The rules are
// semgrep data in two languages, proven against a fixture per language on every run; this package
// owns what is scanned, the proof that every rule still matches, and the refusal to pass over a
// file the analyser did not read. The specification is the plan's SPEC-qa-speech-to-text-rules.md.
package sttrules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-speech-to-text-rules exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage
// or the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The rule file and its two fixtures, relative to the repository root.
const (
	ruleFile      = ".semgrep/speech-to-text.yml"
	jsFixtureFile = ".semgrep/speech-to-text.js"
	pyFixtureFile = ".semgrep/speech-to-text.py"
)

// The options that take a value.
const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Timeouts bound each process.
const (
	versionTimeout = time.Minute
	semgrepTimeout = 15 * time.Minute
)

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts git (through discovery) and semgrep.
	Runner proc.Runner
}

const usageText = `usage: qa-speech-to-text-rules [--root DIR] [--scripts DIR] [PATH...]

The rules in .semgrep/speech-to-text.yml (a model judged present without its weights, a dropdown
label that explains itself) over every JavaScript and Python file git would publish, after proving
each rule against its fixture in .semgrep/.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  PATH            judge these files only (relative to the root) instead of every one
  -h, --help      this text

Exit codes: 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage or the run could
not complete (semgrep missing or broken, a file handed to it not scanned).
`

// options is the parsed command line.
type options struct {
	help    bool
	root    string
	scripts string
	paths   []string
}

// parseArgs reads the command line; an unknown option is a usage error.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == flagRoot || arg == flagScripts:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == flagRoot {
				opts.root = args[i]
			} else {
				opts.scripts = args[i]
			}
		case len(arg) > 0 && arg[0] == '-':
			return opts, fmt.Errorf("unknown argument '%s'", arg)
		default:
			opts.paths = append(opts.paths, arg)
		}
	}
	return opts, nil
}

// failure is a run that ended early with an exit code of its own.
type failure struct {
	code ExitCode
	msg  string
}

func (f *failure) Error() string { return f.msg }

// failed is a run that ends early with exit 1: a rule that failed its proof.
func failed(format string, args ...any) error {
	return &failure{code: ExitFailed, msg: fmt.Sprintf(format, args...)}
}

// gate is one run.
type gate struct {
	env     Env
	root    string
	scripts string
	stdout  bytes.Buffer
	// verdict is the exit status of a run that completed.
	verdict ExitCode
}

// Main runs the gate and returns the exit status.
func Main(args []string, env Env) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		return report(env, err, true)
	}
	if opts.help {
		return write(env, usageText)
	}
	root, err := rootpath.Root(env.Cwd, opts.root, opts.scripts)
	if err != nil {
		return report(env, err, false)
	}
	g := &gate{env: env, root: root, scripts: filepath.Join(root, "scripts")}
	if opts.scripts != "" {
		g.scripts = opts.scripts
	}
	if err := g.run(opts.paths); err != nil {
		return report(env, err, false)
	}
	if _, err := env.Stdout.Write(g.stdout.Bytes()); err != nil {
		return report(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return g.verdict
}

// write sends text to stdout; the status is that of the write.
func write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return report(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// report prints an ERROR line (and the usage, for a usage error) on stderr and returns the exit
// code of the error: that of a failure, else 2. A stderr that cannot be written to has nowhere
// left to say so; the status is the same either way.
func report(env Env, err error, usage bool) ExitCode {
	code := ExitCannotRun
	if f, ok := errors.AsType[*failure](err); ok {
		code = f.code
	}
	text := "ERROR: " + err.Error() + "\n"
	if usage {
		text += usageText
	}
	written, werr := io.WriteString(env.Stderr, text)
	return statusAfter(written, werr, code)
}

// statusAfter is the status of a run that ended early, whether or not its explanation could be
// written.
func statusAfter(_ int, _ error, code ExitCode) ExitCode { return code }

// run is the whole check; a run that ends early is an error, one that completes sets the verdict.
func (g *gate) run(paths []string) error {
	ctx := context.Background()
	if err := g.requireSemgrep(ctx); err != nil {
		return err
	}
	rules, err := g.loadRules()
	if err != nil {
		return err
	}
	if err = g.proveRules(ctx, rules); err != nil {
		return err
	}
	sets, err := g.files(paths)
	if err != nil {
		return err
	}
	got, err := g.scan(ctx, rules, sets)
	if err != nil {
		return err
	}
	g.verdict = g.finish(got)
	return nil
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

// scrub is semgrep's own text with the checkout path taken out: it names files by their absolute
// path, and no checkout path may reach the output.
func (g *gate) scrub(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, g.root+"/", ""), g.root, ".")
}

// path is a file of the repository under the root.
func (g *gate) path(rel string) string {
	return filepath.Join(g.root, filepath.FromSlash(rel))
}
