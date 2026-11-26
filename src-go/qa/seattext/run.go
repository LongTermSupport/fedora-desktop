package seattext

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

// ExitCode is the status qa-seat-text exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a finding, 2 the run could not judge.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// allowedFile is the allowlist, relative to the repository root.
const allowedFile = "src-go/qa/seattext/allowed.tsv"

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
	// Published lists the files git would publish under ScanPaths, as slash paths from the root.
	Published func(root string) ([]string, error)
}

const usageText = `usage: qa-seat-text [--root DIR] [--scripts DIR]

The gate seat-number-text (Plan 00265 R90): no doc, embedded help or launcher message tells a
person to type or pick a seat number. Agent-facing and machine-facing lines that keep one are
rows of src-go/qa/seattext/allowed.tsv, with their reasons.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not judge.
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
	g, err := load(env, root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	return g.judge(out, errOut)
}

// allowed is one allowlist row: a line of path holding literal keeps its seat, for reason.
type allowed struct {
	line                  int
	path, literal, reason string
}

func parseAllowed(data string) ([]allowed, error) {
	var rows []allowed
	for i, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || slices.ContainsFunc(fields, func(f string) bool { return strings.TrimSpace(f) == "" }) {
			return nil, fmt.Errorf("line %d: want 3 non-empty tab-separated fields (path, literal, reason)", i+1)
		}
		rows = append(rows, allowed{line: i + 1, path: fields[0], literal: fields[1], reason: fields[2]})
	}
	return rows, nil
}

// gate is one run's inputs.
type gate struct {
	allowed []allowed
	files   []string
	texts   map[string]string
}

func load(env Env, root string) (*gate, error) {
	published, err := env.Published(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under %s: %w", root, err)
	}
	fsys := env.Open(root)
	g := &gate{texts: map[string]string{}}
	data, err := fs.ReadFile(fsys, allowedFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", allowedFile, err)
	}
	if g.allowed, err = parseAllowed(string(data)); err != nil {
		return nil, fmt.Errorf("%s: %w", allowedFile, err)
	}
	for _, p := range published {
		if InScope(p) != KindNone {
			g.files = append(g.files, p)
		}
	}
	slices.Sort(g.files)
	g.files = slices.Compact(g.files)
	if len(g.files) == 0 {
		return nil, errors.New("no file is in scope: a scan of nothing proves nothing (has the scope moved?)")
	}
	for _, p := range g.files {
		text, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", p, err)
		}
		g.texts[p] = string(text)
	}
	return g, nil
}

// judge reports every finding and bad row on stderr, or the pass on stdout.
func (g *gate) judge(out, errOut *emit.Emitter) ExitCode {
	rowLines := make([][]int, len(g.allowed))
	findings, files := 0, 0
	for _, p := range g.files {
		n := 0
		for _, l := range Lines(InScope(p), g.texts[p]) {
			what := Finding(l.Template, l.Operands, false)
			if what == "" || g.covered(p, l, rowLines) {
				continue
			}
			n++
			errOut.Line(fmt.Sprintf("%s:%d: %s: %s", p, l.No, what, strings.TrimSpace(l.Text)))
		}
		findings += n
		if n > 0 {
			files++
		}
	}
	bad := 0
	for i, row := range g.allowed {
		switch len(rowLines[i]) {
		case 0:
			bad++
			errOut.Line(fmt.Sprintf("%s:%d: stale: %q allows no seat number in %s", allowedFile, row.line, row.literal, row.path))
		case 1:
		default:
			bad++
			errOut.Line(fmt.Sprintf("%s:%d: %q allows lines %v of %s: one row per line", allowedFile, row.line, row.literal, rowLines[i], row.path))
		}
	}
	if findings == 0 && bad == 0 {
		out.Line(fmt.Sprintf("✓ seat-number-text: %d file(s) ask a person for no seat number (%d allowed line(s))", len(g.files), len(g.allowed)))
		return ExitOK
	}
	errOut.Line("")
	errOut.Line(fmt.Sprintf("✗ seat-number-text: %d finding(s) in %d file(s), %d allowlist row(s) stale or covering more than one line", findings, files, bad))
	errOut.Line(Fix + ".")
	errOut.Line("A line only agents or the machine read keeps its seat as a row of " + allowedFile + ", with its reason.")
	return ExitFailed
}

// covered is whether an allowlist row of p covers the line, recording the line for each row.
func (g *gate) covered(p string, l Line, rowLines [][]int) bool {
	hit := false
	for i, row := range g.allowed {
		if row.path == p && strings.Contains(l.Text, row.literal) {
			hit = true
			rowLines[i] = append(rowLines[i], l.No)
		}
	}
	return hit
}
