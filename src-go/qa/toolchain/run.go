// Package toolchain is the gate "the QA tools on this machine are the versions the repository
// pins". A gate's verdict belongs to the binary that produced it, so .qa-versions pins ruff,
// semgrep, shellcheck and golangci-lint, and the higher go directive of src-go/go.mod and
// src-go/qa/go.mod is a floor for go.
// Exit 1 says the machine is wrong (a tool absent, unreadable or drifted); exit 2 says the gate
// could not judge (its pins, go.mod or command line). Under qa-all either fails only this gate.
// The specification is the plan's SPEC-qa-toolchain.md.
package toolchain

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
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
and that go is not older than the higher go directive of src-go/go.mod and src-go/qa/go.mod.

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

func parseArgs(args []string) (options, error) {
	parsed, err := rootargs.Strict(args)
	return options{help: parsed.Help, root: parsed.Root, scripts: parsed.Scripts}, err
}

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	opts, err := parseArgs(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if opts.help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, opts.root, opts.scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	fsys := env.Open(root)
	pins, err := readPins(fsys)
	if err != nil {
		for line := range strings.SplitSeq(err.Error(), "\n") {
			errOut.Line("ERROR: " + line)
		}
		return ExitCannotRun
	}
	floor, err := readGoFloor(fsys)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return judge(env, root, pins, floor, out, errOut)
}

// judge asserts every tool, then answers: all mismatches are reported, not the first.
func judge(env Env, root string, pins map[string]string, floor goFloor, out, errOut *emit.Emitter) ExitCode {
	a := &assertion{env: env, root: root, errOut: errOut}
	for _, t := range pinnedTools {
		a.pinned(t, pins[t.key])
	}
	a.goFloor(floor)
	if a.mismatches > 0 {
		errOut.Line("")
		for _, line := range remedy {
			errOut.Line(line)
		}
		return ExitFailed
	}
	out.Line(fmt.Sprintf("TOOLCHAIN-OK %d tool(s) match .qa-versions", a.checked))
	return ExitOK
}

var remedy = []string{
	"  These gates are version-dependent, so this machine would report a",
	"  different verdict from the CCY container and from CI. Bring it into line:",
	"    host      ./playbooks/imports/play-python.yml   (installs golangci-lint; go via the play-golang.yml it imports)",
	"    container ccy --rebuild   (the image bakes .qa-versions at build time)",
}
