package nokill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-nokill-containerwatch exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a forbidden call site, 2 the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

const (
	// watchdogDir holds the Python watchdog; every published .py below it is judged.
	watchdogDir = "helpers/containerwatch/"
	// containmentPath is the one file allowed to build a stop.
	containmentPath = watchdogDir + "containment.py"
)

// javascriptTargets are the panel code that reads the watchdog's report. They are NAMED, not
// globbed: a glob over a directory that has moved matches nothing and the gate goes on
// printing a pass while scanning no JavaScript.
var javascriptTargets = []string{
	"extensions/fedora-desktop@fedora-desktop/containerReport.js",
	"extensions/fedora-desktop@fedora-desktop/sections/containers.js",
}

// ScanPaths are the paths Env.Published lists files under: the watchdog directory and the named
// JavaScript.
func ScanPaths() []string {
	return append([]string{strings.TrimSuffix(watchdogDir, "/")}, javascriptTargets...)
}

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
	// Published lists the files git would publish (tracked, or untracked and not ignored) under
	// the watchdog's directories, as slash paths relative to the root.
	Published func(root string) ([]string, error)
}

const usageText = `usage: qa-nokill-containerwatch [--root DIR] [--scripts DIR]

L0 no-kill gate for the container-watch watchdog (helpers/containerwatch/**/*.py and the panel's
containerReport.js and sections/containers.js): no executable call site may signal a process or
end a container. helpers/containerwatch/containment.py alone may build a graceful stop.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a forbidden call site, 2 bad usage or the run could not complete.
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
	published, err := env.Published(root)
	if err != nil {
		errOut.Line("ERROR: cannot list the files git would publish under " + root + ": " + err.Error())
		return ExitCannotRun
	}
	targets, ok := collectTargets(published, errOut)
	if !ok {
		return ExitCannotRun
	}
	findings, readable := judgeAll(env.Open(root), targets, errOut)
	if !readable {
		return ExitCannotRun
	}
	return report(findings, targets, out, errOut)
}

// collectTargets is the files to judge, in path order: every published .py below the watchdog
// directory, and the named JavaScript. false, with every problem on stderr, when a declared
// target is not published or there is no Python at all: a gate that has stopped judging a
// file must not pass.
func collectTargets(published []string, errOut *emit.Emitter) ([]string, bool) {
	var targets []string
	for _, p := range published {
		if strings.HasPrefix(p, watchdogDir) && strings.HasSuffix(p, ".py") {
			targets = append(targets, p)
		}
	}
	ok := true
	if len(targets) == 0 {
		errOut.Line("ERROR: no Python file under " + watchdogDir + " is published; the watchdog must exist")
		ok = false
	}
	for _, js := range javascriptTargets {
		if slices.Contains(published, js) {
			targets = append(targets, js)
			continue
		}
		errOut.Line("ERROR: declared container-watch JavaScript target is missing: " + js)
		ok = false
	}
	if !ok {
		errOut.Line("✗ no-kill gate: refusing to pass on a scan that did not cover every target.")
		errOut.Line("  Re-point javascriptTargets if the file moved.")
		return nil, false
	}
	slices.Sort(targets)
	return slices.Compact(targets), true
}

// judgeAll reads and judges every target. false, with each unreadable file on stderr, when any
// cannot be read.
func judgeAll(fsys fs.FS, targets []string, errOut *emit.Emitter) ([]Finding, bool) {
	var findings []Finding
	readable := true
	for _, rel := range targets {
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			errOut.Line(fmt.Sprintf("ERROR: cannot read %s: %v", rel, err))
			readable = false
			continue
		}
		findings = append(findings, Judge(rel, string(data), rel == containmentPath)...)
	}
	return findings, readable
}

// report prints the verdict: violations and guidance on stderr, a pass on stdout.
func report(findings []Finding, targets []string, out, errOut *emit.Emitter) ExitCode {
	if len(findings) == 0 {
		if slices.Contains(targets, containmentPath) {
			out.Line("✓ containment.py: stop-only confirmed (the one audited exception)")
		}
		out.Line(fmt.Sprintf("✓ no-kill gate: %d container-watch file(s) clean — no process-termination call site", len(targets)))
		return ExitOK
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[f.Path] = true
		errOut.Line(fmt.Sprintf("%s:%d: (%s) %s", f.Path, f.Line, f.Rule, f.Text))
	}
	errOut.Line("")
	errOut.Line(fmt.Sprintf("✗ no-kill gate: %d forbidden call site(s) in %d file(s)", len(findings), len(files)))
	errOut.Line("The watchdog must never signal a process, and only containment.py may ask")
	errOut.Line("an engine to stop a container. Remove the termination call.")
	errOut.Line("The word 'kill' is allowed ONLY inside guidance string literals (exec_hint),")
	errOut.Line("not as an executable call site.")
	return ExitFailed
}
