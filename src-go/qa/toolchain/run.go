// Package toolchain is the gate "the QA tools on this machine are the versions the repository
// pins". A gate's verdict belongs to the binary that produced it, so .qa-versions pins ruff,
// semgrep, shellcheck and golangci-lint, and the go directive of src-go/go.mod is a floor for go.
// Tool drift is exit 1, never 2: exit 2 makes qa-all abort the whole run, which would take every
// unrelated gate offline for one drifted tool. The specification is the plan's
// SPEC-qa-toolchain.md.
package toolchain

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-toolchain exits with.
type ExitCode int

// The statuses callers read: 0 every tool matches, 1 a tool is absent, unreadable or at another
// version, 2 the gate could not judge.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// defaultTimeout bounds each version run when Env.Timeout is zero.
const defaultTimeout = time.Minute

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
	// LookPath finds a tool on PATH (exec.LookPath).
	LookPath func(name string) (string, error)
	// Runner starts the version runs.
	Runner proc.Runner
	// Timeout bounds each version run; zero is defaultTimeout.
	Timeout time.Duration
}

const usageText = `usage: qa-toolchain [--root DIR] [--scripts DIR]

Asserts that ruff, semgrep, shellcheck and golangci-lint are the versions pinned in .qa-versions
and that go is not older than the go directive of src-go/go.mod.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 every tool matches, 1 a tool is absent, unreadable or at another version, 2 bad
usage or the pins could not be read. stdout is one TOOLCHAIN-OK line; diagnostics go to stderr.
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
	fsys := env.Open(root)
	pins, err := readPins(fsys)
	if err != nil {
		for line := range strings.SplitSeq(err.Error(), "\n") {
			errOut.line("ERROR: " + line)
		}
		return ExitCannotRun
	}
	floor, err := readGoFloor(fsys)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return judge(env, root, pins, floor, out, errOut)
}

// judge asserts every tool, then answers: all mismatches are reported, not the first.
func judge(env Env, root string, pins map[string]string, floor string, out, errOut *emitter) ExitCode {
	a := &assertion{env: env, root: root, errOut: errOut}
	for _, t := range pinnedTools {
		a.pinned(t, pins[t.key])
	}
	a.goFloor(floor)
	if a.mismatches > 0 {
		errOut.line("")
		for _, line := range remedy {
			errOut.line(line)
		}
		return ExitFailed
	}
	out.line(fmt.Sprintf("TOOLCHAIN-OK %d tool(s) match .qa-versions", a.checked))
	return ExitOK
}

var remedy = []string{
	"  These gates are version-dependent, so this machine would report a",
	"  different verdict from the CCY container and from CI. Bring it into line:",
	"    host      ./playbooks/imports/play-python.yml   (installs golangci-lint; go via the play-golang.yml it imports)",
	"    container ccy --rebuild   (the image bakes .qa-versions at build time)",
}
