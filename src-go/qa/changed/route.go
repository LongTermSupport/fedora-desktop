package changed

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// The matcher kinds that are not a glob list, and the tree list names.
const (
	kindPy     = "@py"
	kindSh     = "@sh"
	kindListed = "@listed"
	kindGlob   = "glob"
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
	case field == kindPy || field == kindSh:
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
	return m.kind == kindPy || m.kind == kindSh
}

// matches is whether rel is matched; listed is the set of paths on the file @listed names.
func (m matcher) matches(rel string, probe fileProbe, listed map[string]bool) (bool, error) {
	switch m.kind {
	case kindPy:
		return probe.isPython(rel)
	case kindSh:
		return probe.isShell(rel)
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
	for _, rl := range slices.Concat(r.also, r.first) {
		for _, id := range rl.checks {
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

// routeColumns is the width of a rule row: phase, matcher, checks, flags.
const routeColumns = 4

func (r *routing) add(rw row) error {
	if rw.fields[0] == "perfile-gate" {
		names := strings.Fields(strings.Join(rw.fields[1:], " "))
		if len(names) == 0 {
			return errors.New("perfile-gate names no gate")
		}
		r.perFileGates = append(r.perFileGates, names...)
		return nil
	}
	if len(rw.fields) != routeColumns {
		return fmt.Errorf("want %d tab-separated columns (phase, match, checks, flags), got %d", routeColumns, len(rw.fields))
	}
	phase, matchField, checkField, flagField := rw.fields[0], rw.fields[1], rw.fields[2], rw.fields[3]
	if phase == "post" {
		return r.addPost(rw.line, matchField, checkField)
	}
	if phase != "also" && phase != "first" {
		return fmt.Errorf("unknown phase %q (want also, first, post or perfile-gate)", phase)
	}
	m, err := parseMatcher(matchField)
	if err != nil {
		return err
	}
	rl := rule{line: rw.line, m: m}
	if checkField == "@yaml" {
		if phase != "first" {
			return errors.New("@yaml belongs to a first rule")
		}
		rl.yaml = true
	} else if rl.checks, err = parseChecks(checkField); err != nil {
		return err
	}
	if err := rl.parseFlags(flagField); err != nil {
		return err
	}
	if phase == "also" {
		r.also = append(r.also, rl)
	} else {
		r.first = append(r.first, rl)
	}
	return nil
}

func (r *routing) addPost(line int, condField, checkField string) error {
	var anyOf []string
	for cond := range strings.SplitSeq(condField, ",") {
		kind, ok := strings.CutPrefix(cond, "any:")
		if !ok || (kind != kindSh && kind != kindPy) {
			return fmt.Errorf("unknown post condition %q (want any:@sh or any:@py)", cond)
		}
		anyOf = append(anyOf, kind)
	}
	checks, err := parseChecks(checkField)
	if err != nil {
		return err
	}
	r.post = append(r.post, postRule{line: line, anyOf: anyOf, checks: checks})
	return nil
}

// parseChecks reads a comma-separated list of check ids; "-" is none.
func parseChecks(field string) ([]string, error) {
	if field == "-" {
		return nil, nil
	}
	ids := strings.Split(field, ",")
	for _, id := range ids {
		if !isCheckID(id) {
			return nil, fmt.Errorf("unknown check %q", id)
		}
	}
	return ids, nil
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

// trees are the excluded directory lists, from scripts/qa-trees.tsv, which qa-discovery.bash
// reads too so the bash gates and this tool share one list.
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
