package changed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// pickedSuite is a test suite chosen to run, with why.
type pickedSuite struct {
	name string // scripts/<name>.bash
	why  []string
	// rows are the gate-table rows of the suite's script; its output is judged by each one's
	// readers, as qa-all judges that gate.
	rows []gates.Gate
}

// rowsByScript maps each gate script of scripts/qa-gates.tsv to its rows: a suite picked from the
// table is judged by its row's own readers, exactly as qa-all judges that gate.
func rowsByScript(scripts string) (map[string][]gates.Gate, error) {
	table, err := gates.Load(filepath.Join(scripts, "qa-gates.tsv"))
	if err != nil {
		return nil, err
	}
	rows := map[string][]gates.Gate{}
	for _, g := range table {
		if name, ok := g.Script(); ok {
			rows[name] = append(rows[name], g)
		}
	}
	return rows, nil
}

// loadPopulation is the suites that may run: the scripts of the rows of scripts/qa-gates.tsv,
// the one list of what qa-all runs, minus the per-file gates. A script that is gone would never
// be picked, silently, so it is an error here.
func loadPopulation(scripts string, perFileGates []string) ([]string, error) {
	table, err := gates.Load(filepath.Join(scripts, "qa-gates.tsv"))
	if err != nil {
		return nil, err
	}
	population := gates.Population(table, perFileGates)
	for _, name := range population {
		if regular, err := isRegular(filepath.Join(scripts, name)); err != nil {
			return nil, err
		} else if !regular {
			return nil, fmt.Errorf("qa-gates.tsv runs %s, which is not a file in %s: a suite that is gone would never be picked", name, scripts)
		}
	}
	if len(population) == 0 {
		return nil, errors.New("found no suites in qa-gates.tsv: every row is a per-file gate; fix qa-changed")
	}
	return population, nil
}

// goRow is one command of scripts/go-binary-suites.tsv: the suites that drive the binary, or
// none when its oracle is a Go test the Go gate runs whole (the word qa-go).
type goRow struct {
	command string
	suites  []string // scripts/<name>.bash
}

// goTestOracle is the suites column of a command whose oracle is a Go test.
const goTestOracle = "qa-go"

// loadGoRows reads the table of deployed Go commands. A suite that is misspelt, deleted or
// not in the population would never be picked, silently, so each is an error here.
func loadGoRows(path string, population []string) ([]goRow, error) {
	rows, err := readRows(path)
	if err != nil {
		return nil, err
	}
	var out []goRow
	for _, rw := range rows {
		if len(rw.fields) != 2 || rw.fields[0] == "" || strings.TrimSpace(rw.fields[1]) == "" {
			return nil, fmt.Errorf("%s:%d: want <command><TAB><suites>", path, rw.line)
		}
		suites := strings.Fields(rw.fields[1])
		if len(suites) == 1 && suites[0] == goTestOracle {
			out = append(out, goRow{command: rw.fields[0]})
			continue
		}
		for _, suite := range suites {
			if name, ok := strings.CutPrefix(suite, "scripts/"); !ok || !slices.Contains(population, name) {
				return nil, fmt.Errorf("%s:%d: suite %q is not one qa-gates.tsv lists (a misspelt or deleted suite would never be picked)", path, rw.line, suite)
			}
		}
		out = append(out, goRow{command: rw.fields[0], suites: suites})
	}
	return out, nil
}

// closureDirs is the directories of a command's build closure, from the go tool, so it cannot
// drift from the imports.
func (s *session) closureDirs(ctx context.Context, command string) ([]string, error) {
	res, err := s.runCmd(ctx, proc.Cmd{
		Argv: []string{"go", "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", "./cmd/" + command},
		Dir:  filepath.Join(s.root, "src-go"),
	})
	if err != nil {
		return nil, fmt.Errorf("go list -deps ./cmd/%s failed; cannot map src-go/ changes to the %s suites: %w", command, command, err)
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("go list -deps ./cmd/%s failed; cannot map src-go/ changes to the %s suites: %s", command, command, strings.TrimSpace(res.Stderr))
	}
	var dirs []string
	for dir := range strings.SplitSeq(res.Stdout, "\n") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// ownedBy is whether a src-go file is in a build closure: go.mod and go.sum always are; any
// other file when it lies under one of the closure's directories (so a test file, testdata or
// an embedded asset of a closure package is owned too).
func (s *session) ownedBy(rel string, dirs []string) bool {
	if rel == "src-go/go.mod" || rel == "src-go/go.sum" {
		return true
	}
	abs := filepath.Join(s.root, rel)
	return slices.ContainsFunc(dirs, func(dir string) bool { return strings.HasPrefix(abs, dir+"/") })
}

// buildOwners maps each command suite to the changed src-go files in its command's closure.
func (s *session) buildOwners(ctx context.Context, rows []goRow) (map[string][]string, error) {
	owners := map[string][]string{}
	if len(s.plan.goFiles) == 0 {
		return owners, nil
	}
	for _, row := range rows {
		if len(row.suites) == 0 {
			continue
		}
		dirs, err := s.closureDirs(ctx, row.command)
		if err != nil {
			return nil, err
		}
		for _, f := range s.plan.goFiles {
			if s.ownedBy(f, dirs) {
				for _, suite := range row.suites {
					owners[suite] = append(owners[suite], f)
				}
			}
		}
	}
	return owners, nil
}

// selectSuites picks the suites the changed files need, in population order: a suite that
// builds from a changed file, that changed itself, or whose text names a changed file by path
// or basename (the path as a substring, the basename as a token: not inside a longer word or
// name). Naming covers the file. It then settles which changed files nothing covers.
func (s *session) selectSuites(ctx context.Context) error {
	population, err := loadPopulation(s.scripts, s.routing.perFileGates)
	if err != nil {
		return err
	}
	rows, err := loadGoRows(filepath.Join(s.scripts, "go-binary-suites.tsv"), population)
	if err != nil {
		return err
	}
	owners, err := s.buildOwners(ctx, rows)
	if err != nil {
		return err
	}
	gateRows, err := rowsByScript(s.scripts)
	if err != nil {
		return err
	}
	changed := slices.Concat(s.plan.existing, s.plan.deleted)
	for _, name := range population {
		suite := "scripts/" + name
		text, err := os.ReadFile(filepath.Join(s.scripts, name))
		if err != nil {
			return fmt.Errorf("cannot read suite %s: %w", suite, err)
		}
		var why []string
		for _, f := range owners[suite] {
			why = append(why, "builds from "+f)
		}
		if slices.Contains(changed, suite) {
			why = append(why, "the suite itself changed")
			s.plan.covered[suite] = true
		}
		for _, f := range changed {
			if f != suite && !strings.HasSuffix(f, ".md") && namesFile(string(text), f) {
				why = append(why, "names "+f)
				s.plan.covered[f] = true
			}
		}
		if len(why) > 0 {
			s.plan.suites = append(s.plan.suites, pickedSuite{name: suite, why: why, rows: gateRows[name]})
		}
	}
	s.settleUncovered()
	return nil
}

// settleUncovered names the changed files no rule, suite or exclusion accounts for.
func (s *session) settleUncovered() {
	for _, f := range slices.Concat(s.plan.existing, s.plan.deleted) {
		if !s.plan.covered[f] && !slices.Contains(s.plan.skipped, f) {
			s.plan.uncovered = append(s.plan.uncovered, f)
		}
	}
}

// namesFile is whether the text mentions the file by path or by basename as a token: a basename
// inside a longer word ("cc" in "accept") is not a mention.
func namesFile(text, rel string) bool {
	if strings.Contains(text, rel) {
		return true
	}
	base := filepath.Base(rel)
	for from := 0; ; {
		i := strings.Index(text[from:], base)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(base)
		if (start == 0 || !isNameByte(text[start-1])) && (end == len(text) || !isNameByte(text[end])) {
			return true
		}
		from = start + 1
	}
}

// isNameByte is a byte that continues a word or a file or command name.
func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
