// Package teamchatseparate is the static gate teamchat-keeps-off-v1 (Plan 00209,
// DESIGN-v2-D-migration.md section 4.1 part 1): no file v2 owns holds a v1 name.
// scripts/qa-teamchat-separate.bash builds and runs its command, qa-teamchat-separate.
//
// The expectations, each held by a test of the same number:
//
//   - K1 The command line is rootargs' (-h, --root DIR, --scripts DIR); anything else is exit 2
//     with the usage text.
//   - K2 A tree whose v2 files hold no v1 name passes: exit 0, one line on stdout counting the
//     v2 files, the names listed and the allowed references.
//   - K3 Every line of every file in scope is matched against the v1 name list v1-names.txt (its
//     format is in its header). Each name found on a line is a finding, printed as
//     path:line: v1 name NAME: text on stderr; any finding is exit 1.
//   - K4 The scope is what git would publish (tracked, or untracked and not ignored) that v2
//     owns: InScope. The two named homes of deliberate v1 references, the v1names package and
//     the migration's v1 adapter, are out of it, as is everything v2 does not own.
//   - K5 allowed.tsv lists the remaining references (path, literal, reason), one row per
//     reference: a finding on a line of the path that an occurrence of the literal covers is
//     allowed. A row that allows nothing is stale, and a row that allows names on more than one
//     line covers more than its one reference; each is a finding, so the list cannot outlive or
//     outgrow what it allows.
//   - K6 A run that cannot judge is exit 2, never a pass: git failing, no v2 file in scope, the
//     name list missing, empty or malformed, the allowlist missing or malformed, a scoped file
//     unreadable, or stdout or stderr unwritable.
//   - K7 A scoped YAML file names only files the scope holds and the SharedFiles: every tasks/
//     or files/ path References finds on its lines is either, or it is a finding. A v2 task
//     file named without teamchat would otherwise be outside the scope unseen.
//
// D 4.1's part 2, the analyzer over the v1 adapter, is not here: it is a go/analysis rule of
// GoProjectRules.md, built with the adapter. Part 3 is the unit test of
// src-go/internal/teamchat/paths, which reads the same name list.
package teamchatseparate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status qa-teamchat-separate exits with.
type ExitCode int

// The statuses callers read: 0 clean, 1 a v1 reference in a v2 file, 2 the run could not judge.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The gate's data, relative to the repository root.
const (
	packageDir  = "src-go/qa/teamchatseparate/"
	namesFile   = packageDir + "v1-names.txt"
	allowedFile = packageDir + "allowed.tsv"
)

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
	// Published lists the files git would publish under ScanPaths, as slash paths relative to
	// the root.
	Published func(root string) ([]string, error)
}

const usageText = `usage: qa-teamchat-separate [--root DIR] [--scripts DIR]

The gate teamchat-keeps-off-v1: no file v2 (teamchat) owns may hold a v1 name of
src-go/qa/teamchatseparate/v1-names.txt, bar the references allowed.tsv lists. The v1names
package and the migration's v1 adapter are the named homes of deliberate v1 references.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a v1 reference in a v2 file, 2 bad usage or the run could not judge.
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

// gate is one run's inputs: the v1 names, the allowlist, and the v2 files with their text.
type gate struct {
	names   []Name
	allowed []Allowed
	files   []string
	texts   map[string]string
}

// load reads everything a judgement needs, and refuses a run that could not judge.
func load(env Env, root string) (*gate, error) {
	published, err := env.Published(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under %s: %w", root, err)
	}
	fsys := env.Open(root)
	g := &gate{texts: map[string]string{}}
	names, err := readData(fsys, namesFile)
	if err != nil {
		return nil, err
	}
	if g.names, err = ParseNames(names); err != nil {
		return nil, fmt.Errorf("%s: %w", namesFile, err)
	}
	allowed, err := readData(fsys, allowedFile)
	if err != nil {
		return nil, err
	}
	if g.allowed, err = ParseAllowed(allowed); err != nil {
		return nil, fmt.Errorf("%s: %w", allowedFile, err)
	}
	for _, p := range published {
		if InScope(p) {
			g.files = append(g.files, p)
		}
	}
	slices.Sort(g.files)
	g.files = slices.Compact(g.files)
	if len(g.files) == 0 {
		return nil, errors.New("no v2 file is published: a scan of nothing proves nothing (has the scope moved?)")
	}
	for _, p := range g.files {
		if g.texts[p], err = readData(fsys, p); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func readData(fsys fs.FS, rel string) (string, error) {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return string(data), nil
}

// judge reports every finding and bad row on stderr, or the pass on stdout.
func (g *gate) judge(out, errOut *emit.Emitter) ExitCode {
	// rowLines is, for each allowlist row, the lines on which it covered a hit.
	rowLines := make([][]int, len(g.allowed))
	findings, files := 0, 0
	for _, p := range g.files {
		n := g.judgeFile(p, rowLines, errOut)
		findings += n
		if n > 0 {
			files++
		}
	}
	badRows := 0
	for i, row := range g.allowed {
		switch lines := rowLines[i]; {
		case len(lines) == 0:
			badRows++
			errOut.Line(fmt.Sprintf("%s:%d: stale: %s covers no v1 name in %s", allowedFile, row.Line, row.Literal, row.Path))
		case len(lines) > 1:
			badRows++
			errOut.Line(fmt.Sprintf("%s:%d: %s covers v1 names on lines %s of %s: one row per reference",
				allowedFile, row.Line, row.Literal, joinLines(lines), row.Path))
		}
	}
	if findings == 0 && badRows == 0 {
		out.Line(fmt.Sprintf("✓ teamchat-keeps-off-v1: %d v2 file(s) hold no v1 name (%d listed, %d allowed reference(s))",
			len(g.files), len(g.names), len(g.allowed)))
		return ExitOK
	}
	errOut.Line("")
	errOut.Line(fmt.Sprintf("✗ teamchat-keeps-off-v1: %d v1 reference(s) in %d v2 file(s), %d allowlist row(s) stale or covering more than one line",
		findings, files, badRows))
	errOut.Line("v2 touches nothing v1 uses (Plan 00209 D 4.1). Take the name from")
	errOut.Line("src-go/internal/teamchat/paths, keep a v1 constant v2 needs in")
	errOut.Line("src-go/internal/teamchat/v1names, or list a reference that cannot live there in")
	errOut.Line(allowedFile + " with its reason, one row per line; remove a stale row. A v2 play")
	errOut.Line("or task file names only v2's files (teamchat*) and the shared ones scope.go lists.")
	return ExitFailed
}

// joinLines is "3 and 7", or "3, 5 and 7".
func joinLines(lines []int) string {
	parts := make([]string, len(lines))
	for i, n := range lines {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// judgeFile reports the file's findings, records the lines on which each allowlist row covered a
// hit, and returns how many findings it reported.
func (g *gate) judgeFile(p string, rowLines [][]int, errOut *emit.Emitter) int {
	findings := 0
	yaml := strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")
	for i, line := range strings.Split(g.texts[p], "\n") {
		for _, n := range g.names {
			if g.unallowed(p, line, i+1, n.Find(line), rowLines) {
				findings++
				errOut.Line(fmt.Sprintf("%s:%d: v1 name %s: %s", p, i+1, n.Text, strings.TrimSpace(line)))
			}
		}
		if !yaml {
			continue
		}
		for _, ref := range References(line) {
			if _, shared := SharedFiles[ref]; !shared && !InScope(ref) {
				findings++
				errOut.Line(fmt.Sprintf("%s:%d: names %s, which v2 does not own: %s", p, i+1, ref, strings.TrimSpace(line)))
			}
		}
	}
	return findings
}

// unallowed is whether any hit on the line is covered by no allowlist row of the file. Every
// row that covers a hit has the line recorded.
func (g *gate) unallowed(p, line string, lineNo int, hits []Hit, rowLines [][]int) bool {
	bad := false
	for _, h := range hits {
		covered := false
		for i, row := range g.allowed {
			if row.Path == p && row.covers(line, h) {
				covered = true
				if !slices.Contains(rowLines[i], lineNo) {
					rowLines[i] = append(rowLines[i], lineNo)
				}
			}
		}
		bad = bad || !covered
	}
	return bad
}
