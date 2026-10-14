package nokill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
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
	root, err := rootDir(opts, env.Cwd)
	if err != nil {
		errOut.line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	published, err := env.Published(root)
	if err != nil {
		errOut.line("ERROR: cannot list the files git would publish under " + root + ": " + err.Error())
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

// rootDir is the repository root: --root, else the parent of --scripts, else the working
// directory, made absolute.
func rootDir(opts options, cwd string) (string, error) {
	root := cwd
	switch {
	case opts.root != "":
		root = opts.root
	case opts.scripts != "":
		root = filepath.Join(opts.scripts, "..")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the root %s: %w", root, err)
	}
	return abs, nil
}

// collectTargets is the files to judge, in path order: every published .py below the watchdog
// directory, and the named JavaScript. false, with every problem on stderr, when a declared
// target is not published or there is no Python at all: a gate that has stopped judging a
// file must not pass.
func collectTargets(published []string, errOut *emitter) ([]string, bool) {
	var targets []string
	for _, p := range published {
		if strings.HasPrefix(p, watchdogDir) && strings.HasSuffix(p, ".py") {
			targets = append(targets, p)
		}
	}
	ok := true
	if len(targets) == 0 {
		errOut.line("ERROR: no Python file under " + watchdogDir + " is published; the watchdog must exist")
		ok = false
	}
	for _, js := range javascriptTargets {
		if slices.Contains(published, js) {
			targets = append(targets, js)
			continue
		}
		errOut.line("ERROR: declared container-watch JavaScript target is missing: " + js)
		ok = false
	}
	if !ok {
		errOut.line("✗ no-kill gate: refusing to pass on a scan that did not cover every target.")
		errOut.line("  Re-point javascriptTargets if the file moved.")
		return nil, false
	}
	slices.Sort(targets)
	return slices.Compact(targets), true
}

// judgeAll reads and judges every target. false, with each unreadable file on stderr, when any
// cannot be read.
func judgeAll(fsys fs.FS, targets []string, errOut *emitter) ([]Finding, bool) {
	var findings []Finding
	readable := true
	for _, rel := range targets {
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			errOut.line(fmt.Sprintf("ERROR: cannot read %s: %v", rel, err))
			readable = false
			continue
		}
		findings = append(findings, Judge(rel, string(data), rel == containmentPath)...)
	}
	return findings, readable
}

// report prints the verdict: violations and guidance on stderr, a pass on stdout.
func report(findings []Finding, targets []string, out, errOut *emitter) ExitCode {
	if len(findings) == 0 {
		if slices.Contains(targets, containmentPath) {
			out.line("✓ containment.py: stop-only confirmed (the one audited exception)")
		}
		out.line(fmt.Sprintf("✓ no-kill gate: %d container-watch file(s) clean — no process-termination call site", len(targets)))
		return ExitOK
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[f.Path] = true
		errOut.line(fmt.Sprintf("%s:%d: (%s) %s", f.Path, f.Line, f.Rule, f.Text))
	}
	errOut.line("")
	errOut.line(fmt.Sprintf("✗ no-kill gate: %d forbidden call site(s) in %d file(s)", len(findings), len(files)))
	errOut.line("The watchdog must never signal a process, and only containment.py may ask")
	errOut.line("an engine to stop a container. Remove the termination call.")
	errOut.line("The word 'kill' is allowed ONLY inside guidance string literals (exec_hint),")
	errOut.line("not as an executable call site.")
	return ExitFailed
}
