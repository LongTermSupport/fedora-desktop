// Package lxcattach is the gate for the rule lxc-attach-file-stream: a command that reaches
// lxc-attach (by name, through a variable that holds it, or through a function that runs it)
// must not have a standard stream on a file. lxc-attach makes each of its standard streams that
// is not /dev/null the container root's own, so the file a caller redirected into, or read
// from, is lost to the caller: a captured stderr it then cats is Permission denied.
//
// The rule follows the descriptors on the bash syntax tree (mvdan.cc/sh): a statement's own
// redirections and those of every group, loop, subshell or function body around it, a pipe or a
// command or process substitution making its end a pipe, and the dups between them. The
// functions and variables that reach the tool are found over every script judged, so a wrapper a
// plan defines in one file is a wrapper in the file that sources it. A redirection made earlier
// by a bare `exec` is not followed.
//
// Scope: every tracked shell script (the tracked-shell list of qa/discovery), less
// CLAUDE/Plan/Completed/, which is frozen history. The rule is proven against its annotated
// fixture (FixturePath) on every run. Exit 0 clean, 1 a finding or a rule that failed its
// fixture, 2 the run could not complete. CLAUDE/QA.md documents the rule under its identifier.
package lxcattach

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

// ExitCode is the status qa-lxc-attach-streams exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage
// or the run could not complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// frozen is the tree the rule does not judge.
const frozen = "CLAUDE/Plan/Completed/"

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks.
	Resolve rootpath.Eval
	// Open returns the file system rooted at a directory.
	Open func(root string) fs.FS
	// Tracked lists the tracked shell scripts under a root, repository-relative.
	Tracked func(root string) ([]string, error)
}

const usageText = `usage: qa-lxc-attach-streams [--root DIR] [--scripts DIR]

The rule lxc-attach-file-stream: no command that reaches lxc-attach (by name, through a variable
or through a function) may have stdin, stdout or stderr on a file, because lxc-attach hands that
file to the container's root. Every tracked shell script is judged, less CLAUDE/Plan/Completed/;
the rule is first proven against its fixture (.semgrep/lxc-attach-streams.bash).

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage or the run could
not complete.
`

// Main runs the command and returns the exit status.
func Main(args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, run(args, env, out, errOut), ExitCannotRun)
}

func run(args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	parsed, err := rootargs.Strict(args)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
			errOut.Block(usageText)
		}
		return ExitCannotRun
	}
	if parsed.Help {
		out.Block(usageText)
		return ExitOK
	}
	root, err := rootpath.RootVia(env.Resolve, env.Cwd, parsed.Root, parsed.Scripts)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	fsys := env.Open(root)
	if code, msg := proveFixture(fsys); code != ExitOK {
		errOut.Line("ERROR: " + msg)
		return code
	}
	sources, err := load(fsys, env.Tracked, root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	found, err := Analyze(sources)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	if len(found) > 0 {
		errOut.Line(fmt.Sprintf("ERROR: %d lxc-attach stream(s) on a file across %d shell script(s) examined:", len(found), len(sources)))
		for _, f := range found {
			errOut.Line("  " + f.String())
		}
		errOut.Line("")
		errOut.Line(fmt.Sprintf("  Hand lxc-attach pipes: see CLAUDE/QA.md %q.", RuleID))
		return ExitFailed
	}
	out.Line(fmt.Sprintf("LXC-ATTACH-STREAMS-OK: %d shell script(s) examined, no lxc-attach stream on a file", len(sources)))
	return ExitOK
}

// proveFixture runs the rule over its fixture: the lines it reports must be exactly the lines the
// fixture marks, and it must mark some.
func proveFixture(fsys fs.FS) (ExitCode, string) {
	text, err := fs.ReadFile(fsys, FixturePath)
	if err != nil {
		return ExitCannotRun, fmt.Sprintf("cannot read %s: %v", FixturePath, err)
	}
	got, err := FixtureFindings(string(text))
	if err != nil {
		return ExitCannotRun, fmt.Sprintf("the rule cannot read its fixture: %v", err)
	}
	marks := FixtureMarks(string(text))
	if len(marks) == 0 || !slices.Equal(marks, got) {
		return ExitFailed, fmt.Sprintf("the rule failed its fixture %s: the fixture marks lines %v, the rule reports %v", FixturePath, fmtLines(marks), fmtLines(got))
	}
	return ExitOK, ""
}

// fmtLines prints a nil list as [] like an empty one.
func fmtLines(lines []int) []int {
	if lines == nil {
		return []int{}
	}
	return lines
}

// load reads every tracked shell script outside the frozen tree.
func load(fsys fs.FS, tracked func(string) ([]string, error), root string) ([]Source, error) {
	rels, err := tracked(root)
	if err != nil {
		return nil, err
	}
	var sources []Source
	for _, rel := range rels {
		if strings.HasPrefix(rel, frozen) {
			continue
		}
		text, readErr := fs.ReadFile(fsys, rel)
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", rel, readErr)
		}
		sources = append(sources, Source{Path: rel, Text: text})
	}
	if len(sources) == 0 {
		return nil, errors.New("no tracked shell script to judge; this gate would certify nothing")
	}
	return sources, nil
}
