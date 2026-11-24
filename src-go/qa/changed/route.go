package changed

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The matcher kinds that are not a glob list, and the tree list names.
const (
	kindPy     = "@py"
	kindSh     = "@sh"
	kindPyTpl  = "@pytemplate"
	kindListed = "@listed"
	kindGlob   = "glob"
	// kindFile is a post condition only: any changed file that still exists.
	kindFile   = "@file"
	listShell  = "shell"
	listPython = "python"
)

// A matcher decides whether a rule applies to a repo-relative path: a list of globs, one of
// the content predicates @py and @sh, or @listed:<file>, a path that begins a line of a file.
type matcher struct {
	kind   string // "glob", "@py", "@sh" or "@listed"
	globs  []string
	listed string
}

func parseMatcher(field string) (matcher, error) {
	switch {
	case field == kindPy || field == kindSh || field == kindPyTpl:
		return matcher{kind: field}, nil
	case strings.HasPrefix(field, "@listed:"):
		file := strings.TrimPrefix(field, "@listed:")
		if file == "" {
			return matcher{}, fmt.Errorf("%q names no file", field)
		}
		return matcher{kind: kindListed, listed: file}, nil
	case strings.HasPrefix(field, "@"):
		return matcher{}, fmt.Errorf("unknown predicate %q", field)
	}
	return matcher{kind: kindGlob, globs: strings.Split(field, ",")}, nil
}

// contentBased is whether the match reads the file's first line, which a deleted file has not got.
func (m matcher) contentBased() bool {
	return m.kind == kindPy || m.kind == kindSh || m.kind == kindPyTpl
}

// matches is whether rel is matched; listed is the set of paths on the file @listed names.
func (m matcher) matches(rel string, probe fileProbe, listed map[string]bool) (bool, error) {
	switch m.kind {
	case kindPy:
		return probe.isPython(rel)
	case kindSh:
		return probe.isShell(rel)
	case kindPyTpl:
		return probe.isPythonTemplate(rel)
	case kindListed:
		return listed[rel], nil
	}
	for _, g := range m.globs {
		if globMatch(g, rel) {
			return true, nil
		}
	}
	return false, nil
}

// rule is one row of the routing table.
type rule struct {
	line    int
	m       matcher
	checks  []string
	yaml    bool // the row hands the file to the playbook / task / vars logic
	covered bool // a file the rule handles counts as covered
	closure bool // the file joins the src-go list the Go closure is computed for
	tree    string
	rel     string // prefix stripped from the file when the tool runs
	// goPackages are the src-go packages (relative to src-go, "<dir>/..." for a tree) whose
	// tests read the file, for a row that routes a file outside src-go/ to the go check: go-qa's
	// diff mode cannot see such a file, so it is told which packages to run. goFull is a file
	// that can change every Go result, which runs the whole gate.
	goPackages []string
	goFull     bool
	// modules are the test modules the row names (unit-tests:<module>): a sweep that judges every
	// file of a kind, run for any file the row matches, whatever the file is.
	modules []string
}

// postRule adds checks after every file is classified, when any of the named kinds was seen.
type postRule struct {
	line   int
	anyOf  []string
	checks []string
}

// routing is the parsed scripts/qa-routing.tsv: the classification as data.
type routing struct {
	also, first  []rule
	comments     []rule
	post         []postRule
	perFileGates []string
}

// loadRouting parses the routing table strictly: a short row, an unknown phase, check,
// predicate or flag is an error naming the line.
func loadRouting(path string) (*routing, error) {
	rows, err := readRows(path)
	if err != nil {
		return nil, err
	}
	r := &routing{}
	for _, rw := range rows {
		if err := r.add(rw); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, rw.line, err)
		}
		if err := r.checkRels(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, rw.line, err)
		}
	}
	return r, nil
}

// checkRels refuses a rule table in which one check carries two different rel values (a rule
// with no rel counts as the empty one): the plan keeps one rel per check, and the second would
// be dropped silently.
func (r *routing) checkRels() error {
	seen := map[string]string{}
	for _, rl := range slices.Concat(r.also, r.first, r.comments) {
		ids := rl.checks
		if len(rl.modules) > 0 {
			ids = append(slices.Clone(ids), unitTestsID)
		}
		for _, id := range ids {
			if prev, ok := seen[id]; ok && prev != rl.rel {
				return fmt.Errorf("check %s has rel %q here and %q in an earlier rule: one check has one rel", id, rl.rel, prev)
			}
			seen[id] = rl.rel
		}
	}
	return nil
}

type row struct {
	line   int
	fields []string
}

// readRows is the data lines of a tab-separated file, comments and blank lines dropped.
func readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var rows []row
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		text := sc.Text()
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		rows = append(rows, row{n, strings.Split(text, "\t")})
	}
	scanErr := sc.Err()
	closeErr := f.Close()
	if scanErr != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, scanErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	return rows, nil
}

// routeColumns is the width of a rule row: phase, matcher, checks, flags; a row that routes a
// file outside src-go/ to the go check has one more, the Go packages that read it (goColumns).
const (
	routeColumns = 4
	goColumns    = 5
	// goFullRun is the go packages value of a file that can change every Go result.
	goFullRun = "@full"
	// goTree ends a package entry that names every package at or below a directory.
	goTree = "/..."
)

func (r *routing) add(rw row) error {
	if rw.fields[0] == "perfile-gate" {
		names := strings.Fields(strings.Join(rw.fields[1:], " "))
		if len(names) == 0 {
			return errors.New("perfile-gate names no gate")
		}
		r.perFileGates = append(r.perFileGates, names...)
		return nil
	}
	if len(rw.fields) != routeColumns && len(rw.fields) != goColumns {
		return fmt.Errorf("want %d tab-separated columns (phase, match, checks, flags), or %d for a row routing a file outside src-go/ to go (then its Go packages), got %d",
			routeColumns, goColumns, len(rw.fields))
	}
	phase, matchField, checkField, flagField := rw.fields[0], rw.fields[1], rw.fields[2], rw.fields[3]
	if phase == "post" {
		if len(rw.fields) != routeColumns {
			return fmt.Errorf("a post row has %d columns", routeColumns)
		}
		return r.addPost(rw.line, matchField, checkField)
	}
	if phase != "also" && phase != "first" && phase != "comments" {
		return fmt.Errorf("unknown phase %q (want also, first, comments, post or perfile-gate)", phase)
	}
	m, err := parseMatcher(matchField)
	if err != nil {
		return err
	}
	if phase == "comments" && !rowsComparable(m) {
		return fmt.Errorf("a comments row names only files qa-changed can compare by their rows (%s), each by its path", strings.Join(slices.Sorted(maps.Keys(rowComparers)), ", "))
	}
	rl := rule{line: rw.line, m: m}
	if err := rl.parseChecksAndFlags(phase, checkField, flagField); err != nil {
		return err
	}
	if err := rl.parseGoPackages(rw.fields[routeColumns:]); err != nil {
		return err
	}
	switch phase {
	case "also":
		r.also = append(r.also, rl)
	case "comments":
		r.comments = append(r.comments, rl)
	default:
		r.first = append(r.first, rl)
	}
	return nil
}

// parseChecksAndFlags reads an also or first row's checks column (@yaml, or check ids and named
// test modules) and its flags column.
func (rl *rule) parseChecksAndFlags(phase, checkField, flagField string) error {
	if checkField == "@yaml" {
		if phase != "first" {
			return errors.New("@yaml belongs to a first rule")
		}
		rl.yaml = true
	} else {
		var err error
		if rl.checks, rl.modules, err = parseChecks(checkField); err != nil {
			return err
		}
	}
	if err := rl.parseFlags(flagField); err != nil {
		return err
	}
	if len(rl.modules) > 0 && rl.rel != "" {
		return errors.New("a row naming test modules has no rel: a module is not a path")
	}
	return nil
}

func (r *routing) addPost(line int, condField, checkField string) error {
	var anyOf []string
	for cond := range strings.SplitSeq(condField, ",") {
		kind, ok := strings.CutPrefix(cond, "any:")
		if !ok || (kind != kindSh && kind != kindPy && kind != kindFile) {
			return fmt.Errorf("unknown post condition %q (want any:@sh, any:@py or any:@file)", cond)
		}
		anyOf = append(anyOf, kind)
	}
	checks, modules, err := parseChecks(checkField)
	if err != nil {
		return err
	}
	if len(modules) > 0 {
		return errors.New("a post row names no test module")
	}
	r.post = append(r.post, postRule{line: line, anyOf: anyOf, checks: checks})
	return nil
}

// unitTestsID is the check that runs Python test modules; unit-tests:<module> names one.
const unitTestsID = "unit-tests"

// testModule is a dotted test module under tests/ whose last segment is a test_ file.
var testModule = regexp.MustCompile(`^tests(\.[A-Za-z_][A-Za-z0-9_]*)*\.test_[A-Za-z0-9_]+$`)

// parseChecks reads a comma-separated list of check ids, "-" being none, and the test modules
// the list names as unit-tests:<module>.
func parseChecks(field string) (ids, modules []string, err error) {
	if field == "-" {
		return nil, nil, nil
	}
	for entry := range strings.SplitSeq(field, ",") {
		id, module, named := strings.Cut(entry, ":")
		switch {
		case named && id != unitTestsID:
			return nil, nil, fmt.Errorf("check %q takes no module (only %s:<module> names one)", entry, unitTestsID)
		case named && !testModule.MatchString(module):
			return nil, nil, fmt.Errorf("%q names no test module (tests.<package>.test_<name>, dotted)", entry)
		case named && slices.Contains(modules, module):
			return nil, nil, fmt.Errorf("test module %q is named twice", module)
		case named:
			modules = append(modules, module)
		case !isCheckID(id):
			return nil, nil, fmt.Errorf("unknown check %q", id)
		default:
			ids = append(ids, id)
		}
	}
	return ids, modules, nil
}

func (rl *rule) parseFlags(field string) error {
	if field == "-" {
		return nil
	}
	for flag := range strings.SplitSeq(field, ",") {
		name, value, hasValue := strings.Cut(flag, ":")
		switch {
		case flag == "covered":
			rl.covered = true
		case flag == "closure":
			rl.closure = true
		case hasValue && name == "tree" && (value == listShell || value == listPython):
			rl.tree = value
		case hasValue && name == "rel" && value != "":
			rl.rel = value
		default:
			return fmt.Errorf("unknown flag %q", flag)
		}
	}
	return nil
}

// parseGoPackages reads the go packages column (rest is what follows the flags). It is required
// on, and allowed only on, a row that routes to the go check a file go-qa's diff mode cannot see:
// one its matcher can find outside src-go/. The value is @full, or comma-separated package
// directories relative to src-go, each clean and relative, a tree ending in /....
func (rl *rule) parseGoPackages(rest []string) error {
	routesGo := slices.Contains(rl.checks, "go")
	outside := routesGo && rl.m.reachesOutside("src-go/")
	switch {
	case len(rest) == 0 && outside:
		return fmt.Errorf("a row that routes a file outside src-go/ to go names, in a fifth column, the src-go packages that read it (or %s)", goFullRun)
	case len(rest) == 0:
		return nil
	case !routesGo:
		return errors.New("only a row that routes to go has a fifth column (its Go packages)")
	case !outside:
		return errors.New("a row matching only src-go/ files has no Go packages column: go-qa's diff mode sees them")
	case rest[0] == goFullRun:
		rl.goFull = true
		return nil
	}
	for entry := range strings.SplitSeq(rest[0], ",") {
		if err := checkGoPackage(entry); err != nil {
			return err
		}
		if slices.Contains(rl.goPackages, entry) {
			return fmt.Errorf("go package %q is named twice", entry)
		}
		rl.goPackages = append(rl.goPackages, entry)
	}
	return nil
}

// checkGoPackage refuses a package entry that is not a clean directory relative to src-go.
func checkGoPackage(entry string) error {
	dir := strings.TrimSuffix(entry, goTree)
	switch {
	case dir == "" || dir == "." || strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "-") || path.Clean(dir) != dir || dir == ".." || strings.HasPrefix(dir, "../"):
		return fmt.Errorf("go package %q is not a clean directory relative to src-go (internal/x, or internal/x/... for a tree)", entry)
	case strings.Contains(dir, "..."):
		return fmt.Errorf("go package %q holds ... other than at its end", entry)
	case strings.HasPrefix(dir, "src-go/") || strings.HasPrefix(dir, "@"):
		return fmt.Errorf("go package %q: name the directory relative to src-go, or the whole value %s", entry, goFullRun)
	}
	return nil
}

// reachesOutside is whether the matcher can match a file not under prefix: a predicate or a
// listed file can match anywhere, a glob list only if one glob does not start with prefix.
func (m matcher) reachesOutside(prefix string) bool {
	if m.kind != kindGlob {
		return true
	}
	return slices.ContainsFunc(m.globs, func(g string) bool { return !strings.HasPrefix(g, prefix) })
}

// trees are the excluded directory lists, from scripts/qa-trees.tsv, which the Go
// discovery and patterns gates read too, so they and this tool share one list.
type trees struct {
	shell, python treeList
}

func (t trees) byName(name string) treeList {
	if name == listPython {
		return t.python
	}
	return t.shell
}

func loadTrees(path string) (trees, error) {
	rows, err := readRows(path)
	if err != nil {
		return trees{}, err
	}
	var t trees
	for _, rw := range rows {
		if len(rw.fields) != 2 || rw.fields[1] == "" {
			return trees{}, fmt.Errorf("%s:%d: want <list><TAB><dir>", path, rw.line)
		}
		switch rw.fields[0] {
		case listShell:
			t.shell = append(t.shell, rw.fields[1])
		case listPython:
			t.python = append(t.python, rw.fields[1])
		default:
			return trees{}, fmt.Errorf("%s:%d: unknown list %q (want shell or python)", path, rw.line, rw.fields[0])
		}
	}
	return t, nil
}
