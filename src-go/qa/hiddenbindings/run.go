// Package hiddenbindings is the gate qa-hidden-bindings: the rule project_hidden_bindings
// (every action is a visible, named choice; a key is only a shortcut to one) over the
// languages the Go analyzer of the same name cannot read. Shell is judged here, over the
// mvdan.cc/sh syntax tree: fzf --expect, and fzf --bind of a key to anything but navigation
// (src-go/qa/internal/fzfbind decides what navigation is, for both). Python is judged by
// helpers/qa_environment/textual_bindings.py: a Textual binding whose action no Button runs.
// Each half is proven against its marked fixture in tests/fixtures/hidden-bindings on every
// run, and a file that cannot be read or parsed is a failure to run, never a pass.
package hiddenbindings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-hidden-bindings exits with.
type ExitCode int

// 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage or a run that could not
// complete.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// Where the fixtures and the Python half live, relative to the repository root.
const (
	fixtureTree  = "tests/fixtures/hidden-bindings/"
	shFixture    = fixtureTree + "fixture.bash"
	pyFixture    = fixtureTree + "fixture.py"
	helperModule = "helpers.qa_environment.textual_bindings"
	fixtureMark  = "# finding"
)

// helperTimeout bounds the Python half.
const helperTimeout = 5 * time.Minute

const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Env is everything the command touches outside the repository tree.
type Env struct {
	Stdout, Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Runner starts python3 and, through discovery, git.
	Runner proc.Runner
}

const usageText = `usage: qa-hidden-bindings [--root DIR] [--scripts DIR] [PATH...]

The rule project_hidden_bindings outside Go: an action reachable only by a key. Shell files are
judged for fzf --expect and for fzf --bind of a key to anything but navigation; Python files for
a Textual binding whose action no Button runs. Each half is first proven against its fixture in
tests/fixtures/hidden-bindings. CLAUDE/GoProjectRules.md documents the rule.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  PATH            judge these files only (relative to the root) instead of every tracked one
  -h, --help      this text

Exit codes: 0 clean, 1 a finding or a rule that failed its fixture, 2 bad usage or the run could
not complete (python3 missing, a file unread or unparsed).
`

type options struct {
	help          bool
	root, scripts string
	paths         []string
}

func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == flagRoot || arg == flagScripts:
			if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			if arg == flagRoot {
				opts.root = args[i]
			} else {
				opts.scripts = args[i]
			}
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown argument '%s'", arg)
		default:
			opts.paths = append(opts.paths, arg)
		}
	}
	return opts, nil
}

// failure is a run that ended early with exit 1: a rule that failed its proof.
type failure struct{ msg string }

func (f *failure) Error() string { return f.msg }

type gate struct {
	env           Env
	root, scripts string
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
	findings, counts, err := g.run(opts.paths)
	if err != nil {
		return report(env, err, false)
	}
	var out strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&out, "%s:%d: %s\n", f.Path, f.Line, f.Message)
	}
	if len(findings) == 0 {
		fmt.Fprintf(&out, "HIDDEN-BINDINGS-OK %d shell file(s), %d Python file(s)\n", counts[0], counts[1])
	}
	if code := write(env, out.String()); code != ExitOK || len(findings) == 0 {
		return code
	}
	return report(env, &failure{msg: fmt.Sprintf(
		"%d action(s) reachable only by a key; make each a visible choice (CLAUDE/GoProjectRules.md, %s)", len(findings), Rule)}, false)
}

func write(env Env, text string) ExitCode {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return report(env, fmt.Errorf("cannot write to stdout: %w", err), false)
	}
	return ExitOK
}

// report prints an ERROR line (and the usage, for a usage error) on stderr; its status is that
// of the error, or cannot-run when stderr could not be written either.
func report(env Env, err error, usage bool) ExitCode {
	code := ExitCannotRun
	if f, ok := errors.AsType[*failure](err); ok && f != nil {
		code = ExitFailed
	}
	text := "ERROR: " + err.Error() + "\n"
	if usage {
		text += usageText
	}
	if _, werr := io.WriteString(env.Stderr, text); werr != nil {
		return ExitCannotRun
	}
	return code
}

// run proves both halves, then judges the files; counts is the shell and Python files judged.
func (g *gate) run(paths []string) ([]Finding, [2]int, error) {
	ctx := context.Background()
	if err := g.prove(ctx); err != nil {
		return nil, [2]int{}, err
	}
	sh, py, err := g.files(paths)
	if err != nil {
		return nil, [2]int{}, err
	}
	findings, err := g.shell(sh)
	if err != nil {
		return nil, [2]int{}, err
	}
	found, err := g.python(ctx, py)
	if err != nil {
		return nil, [2]int{}, err
	}
	return append(findings, found...), [2]int{len(sh), len(py)}, nil
}

// shell judges the shell files.
func (g *gate) shell(files []string) ([]Finding, error) {
	var findings []Finding
	for _, rel := range files {
		src, err := g.read(rel)
		if err != nil {
			return nil, err
		}
		found, err := ShellFindings(src, rel)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

// prove runs each half against its fixture: the lines it reports must be exactly the marked
// ones. A rule that silently stops matching is a failed run, never "0 findings".
func (g *gate) prove(ctx context.Context) error {
	shSrc, err := g.read(shFixture)
	if err != nil {
		return err
	}
	pySrc, err := g.read(pyFixture)
	if err != nil {
		return err
	}
	shFound, err := ShellFindings(shSrc, shFixture)
	if err != nil {
		return err
	}
	if markErr := matchMarks(shFixture, shSrc, shFound); markErr != nil {
		return markErr
	}
	pyFound, err := g.python(ctx, []string{pyFixture})
	if err != nil {
		return err
	}
	return matchMarks(pyFixture, pySrc, pyFound)
}

func matchMarks(name string, src []byte, found []Finding) error {
	var marks []int
	got := make([]int, 0, len(found))
	for i, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, fixtureMark) {
			marks = append(marks, i+1)
		}
	}
	for _, f := range found {
		got = append(got, f.Line)
	}
	if len(marks) == 0 || !slices.Equal(marks, got) {
		return &failure{msg: fmt.Sprintf("the rule failed its fixture %s: the fixture marks lines %v, the rule reports %v", name, marks, got)}
	}
	return nil
}

// files is the shell and Python files to judge: those named, else every tracked one but the
// fixtures, which carry the pattern on purpose.
func (g *gate) files(paths []string) ([]string, []string, error) {
	if len(paths) > 0 {
		return g.classify(paths)
	}
	notFixture := func(rel string) bool { return strings.HasPrefix(rel, fixtureTree) }
	sh, err := g.discover("tracked-shell")
	if err != nil {
		return nil, nil, err
	}
	py, err := g.discover("tracked-python")
	if err != nil {
		return nil, nil, err
	}
	sh, py = slices.DeleteFunc(sh, notFixture), slices.DeleteFunc(py, notFixture)
	if len(sh) == 0 || len(py) == 0 {
		return nil, nil, fmt.Errorf("discovery found %d shell and %d Python files; nothing would be checked", len(sh), len(py))
	}
	return sh, py, nil
}

// classify sorts named files into shell and Python by name, else by their first line; any
// other file is not this gate's.
func (g *gate) classify(paths []string) ([]string, []string, error) {
	var sh, py []string
	for _, p := range paths {
		rel := filepath.ToSlash(filepath.Clean(p))
		if filepath.IsAbs(p) || rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, nil, fmt.Errorf("%s is outside the repository root", p)
		}
		src, err := g.read(rel)
		if err != nil {
			return nil, nil, err
		}
		first := string(src)
		if end := strings.IndexByte(first, '\n'); end >= 0 {
			first = first[:end]
		}
		switch {
		case strings.HasSuffix(rel, ".py") || discovery.PythonShebang(first):
			py = append(py, rel)
		case strings.HasSuffix(rel, ".bash") || strings.HasSuffix(rel, ".sh") || discovery.ShellShebang(first):
			sh = append(sh, rel)
		}
	}
	return sh, py, nil
}

func (g *gate) discover(list string) ([]string, error) {
	var out, errs bytes.Buffer
	env := discovery.Env{Stdout: &out, Stderr: &errs, Cwd: g.root, Runner: g.env.Runner}
	if code := discovery.Main([]string{flagRoot, g.root, flagScripts, g.scripts, list}, env); code != discovery.ExitOK {
		return nil, errors.New(strings.TrimPrefix(strings.TrimSpace(errs.String()), "ERROR: "))
	}
	var files []string
	for rel := range strings.SplitSeq(out.String(), "\x00") {
		if rel != "" {
			files = append(files, rel)
		}
	}
	return files, nil
}

func (g *gate) read(rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return data, nil
}

// helperReport is what the Python half prints.
type helperReport struct {
	Files    *int `json:"files"`
	Findings []struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Message string `json:"message"`
	} `json:"findings"`
}

// python runs the Python half over files, in one process; it must account for every file.
func (g *gate) python(ctx context.Context, files []string) ([]Finding, error) {
	if len(files) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	argv := append([]string{"python3", "-m", helperModule}, files...)
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.root})
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s could not run (python3 is required): %w", helperModule, err)
	case res.TimedOut:
		return nil, fmt.Errorf("%s did not finish within %s", helperModule, helperTimeout)
	case res.Code != 0:
		return nil, fmt.Errorf("%s exited %d: %s", helperModule, res.Code, strings.TrimSpace(res.Stderr))
	}
	var rep helperReport
	if err := json.Unmarshal([]byte(res.Stdout), &rep); err != nil {
		return nil, fmt.Errorf("%s printed no report: %w", helperModule, err)
	}
	if rep.Files == nil || *rep.Files != len(files) {
		return nil, fmt.Errorf("%s judged a different number of files than the %d it was handed", helperModule, len(files))
	}
	found := make([]Finding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		found = append(found, Finding{Path: f.Path, Line: f.Line, Message: f.Message})
	}
	return found, nil
}
