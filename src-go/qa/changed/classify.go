package changed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// checkPlan is what one check will be given.
type checkPlan struct {
	items []string
	// rel is the prefix stripped from the items when the tool runs (eslint under extensions/).
	rel string
}

// plan is the outcome of classification and suite selection.
type plan struct {
	checks    map[string]*checkPlan
	skipped   []string
	notes     []string
	uncovered []string
	covered   map[string]bool
	// goFiles are the src-go files whose Go build closure may select command suites.
	goFiles []string
	suites  []pickedSuite
	// existing and deleted are the changed files, in collection order.
	existing, deleted []string
}

func (p *plan) add(id, item, rel string) {
	cp := p.checks[id]
	if cp == nil {
		cp = &checkPlan{rel: rel}
		p.checks[id] = cp
	}
	if item != "" && !slices.Contains(cp.items, item) {
		cp.items = append(cp.items, item)
	}
}

// session is one run: the environment, the repository, the loaded tables and the plan.
type session struct {
	env     Env
	opts    options
	root    string
	scripts string
	timeout time.Duration

	routing *routing
	trees   trees
	listed  map[string]bool
	probe   fileProbe

	plan *plan
	// kinds counts the files each content predicate classified, for the post rules.
	kinds map[string]int

	playbooks       []string
	playbooksLoaded bool

	temps    []string
	writeErr error
}

// load reads the routing tables.
func (s *session) load() error {
	var err error
	if s.routing, err = loadRouting(filepath.Join(s.scripts, "qa-routing.tsv")); err != nil {
		return err
	}
	if s.trees, err = loadTrees(filepath.Join(s.scripts, "qa-trees.tsv")); err != nil {
		return err
	}
	s.listed = map[string]bool{}
	for _, r := range s.routing.also {
		if r.m.kind != kindListed {
			continue
		}
		if err := s.readListed(r.m.listed); err != nil {
			return err
		}
	}
	s.probe = fileProbe{root: s.root}
	s.plan = &plan{checks: map[string]*checkPlan{}, covered: map[string]bool{}}
	s.kinds = map[string]int{}
	return nil
}

// readListed adds the paths that begin a line of a scripts-directory file (path, then a tab).
func (s *session) readListed(name string) error {
	body, err := os.ReadFile(filepath.Join(s.scripts, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", name, err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		if tab := strings.IndexByte(line, '\t'); tab > 0 {
			s.listed[line[:tab]] = true
		}
	}
	return nil
}

// classify routes every changed file by the table: the also rules add checks and never stop;
// the first matching first rule classifies the file; the post rules run last.
func (s *session) classify(ctx context.Context, files collected) error {
	s.plan.existing, s.plan.deleted = files.existing, files.deleted
	s.plan.notes = append(s.plan.notes, files.notes...)
	for _, f := range files.existing {
		if err := s.classifyFile(ctx, f, false); err != nil {
			return err
		}
	}
	for _, f := range files.deleted {
		if err := s.classifyFile(ctx, f, true); err != nil {
			return err
		}
	}
	for _, pr := range s.routing.post {
		if slices.ContainsFunc(pr.anyOf, func(kind string) bool { return s.kinds[kind] > 0 }) {
			for _, id := range pr.checks {
				s.plan.add(id, "", "")
			}
		}
	}
	return nil
}

// applies is whether a rule is evaluated for the file: a deleted file has no content to test.
func (s *session) applies(r rule, rel string, deleted bool) (bool, error) {
	if deleted && r.m.contentBased() {
		return false, nil
	}
	return r.m.matches(rel, s.probe, s.listed)
}

func (s *session) classifyFile(ctx context.Context, rel string, deleted bool) error {
	for _, r := range s.routing.also {
		match, err := s.applies(r, rel, deleted)
		if err != nil {
			return err
		}
		if match && (r.tree == "" || !s.trees.byName(r.tree).excludes(rel)) {
			if err := s.apply(ctx, r, rel, deleted); err != nil {
				return err
			}
		}
	}
	for _, r := range s.routing.first {
		match, err := s.applies(r, rel, deleted)
		if err != nil {
			return err
		}
		if !match {
			continue
		}
		if r.tree != "" && s.trees.byName(r.tree).excludes(rel) {
			s.plan.skipped = append(s.plan.skipped, rel)
			return nil
		}
		if !deleted {
			s.kinds[r.m.kind]++
		}
		return s.apply(ctx, r, rel, deleted)
	}
	return nil
}

// apply gives the file the rule's checks. A deleted file has nothing left to check, so it asks
// only for the checks that run whole.
func (s *session) apply(ctx context.Context, r rule, rel string, deleted bool) error {
	added := false
	for _, id := range r.checks {
		if deleted && !slices.Contains(wholeGate, id) {
			continue
		}
		added = true
		if id == "unit-tests" {
			s.addUnitTests(rel)
		} else {
			s.plan.add(id, rel, r.rel)
		}
	}
	if r.yaml && !deleted {
		if err := s.classifyYAML(ctx, rel); err != nil {
			return err
		}
	}
	if r.closure {
		s.plan.goFiles = append(s.plan.goFiles, rel)
	}
	if (r.covered && !deleted) || (deleted && added) {
		s.plan.covered[rel] = true
	}
	return nil
}

var playbookMarker = regexp.MustCompile(`(?m)^[ \t]*-[ \t]+(hosts|import_playbook):`)

func isPlaybookText(text string) bool {
	return playbookMarker.MatchString(text)
}

// ansibleDirs are the top-level directories whose YAML is a task, vars or inventory file some
// playbook reaches; the patterns gate always runs for it.
var ansibleDirs = []string{"playbooks", "tasks", "vars", "environment", "roles"}

// topDir is the first path segment of a path that has more than one.
func topDir(rel string) (string, bool) {
	top, _, nested := strings.Cut(rel, "/")
	return top, nested
}

// classifyYAML is the playbook, task and vars logic that stays code because it computes the
// playbook set: a playbook checks itself; a task or vars file checks every playbook whose text
// names its basename (one level), and says so when none does.
func (s *session) classifyYAML(ctx context.Context, rel string) error {
	body, err := os.ReadFile(filepath.Join(s.root, rel))
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", rel, err)
	}
	if isPlaybookText(string(body)) {
		s.plan.add("ansible-syntax", rel, "")
		s.plan.add("ansible-patterns", "", "")
		s.plan.add("docs", "", "")
		s.plan.covered[rel] = true
		return nil
	}
	top, nested := topDir(rel)
	if !nested || !slices.Contains(ansibleDirs, top) {
		return nil
	}
	s.plan.add("ansible-patterns", "", "")
	s.plan.covered[rel] = true
	if top == "playbooks" {
		s.plan.add("docs", "", "")
	}
	playbooks, err := s.allPlaybooks(ctx)
	if err != nil {
		return err
	}
	base := filepath.Base(rel)
	named := 0
	for _, pb := range playbooks {
		text, err := os.ReadFile(filepath.Join(s.root, pb))
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", pb, err)
		}
		if strings.Contains(string(text), base) {
			s.plan.add("ansible-syntax", pb, "")
			named++
		}
	}
	if named == 0 {
		s.plan.notes = append(s.plan.notes, fmt.Sprintf("%s: no playbook names '%s', so no ansible-syntax target (patterns gate only)", rel, base))
	}
	return nil
}

// allPlaybooks is every tracked, non-excluded YAML file that is a playbook, computed once.
func (s *session) allPlaybooks(ctx context.Context) ([]string, error) {
	if s.playbooksLoaded {
		return s.playbooks, nil
	}
	out, err := s.git(ctx, "ls-files", "-z", "--", "*.yml", "*.yaml")
	if err != nil {
		return nil, fmt.Errorf("cannot list the playbooks: %w", err)
	}
	for _, f := range nulList(out) {
		if s.trees.shell.excludes(f) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(s.root, f))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", f, err)
		}
		if isPlaybookText(string(text)) {
			s.playbooks = append(s.playbooks, f)
		}
	}
	s.playbooksLoaded = true
	return s.playbooks, nil
}

// addUnitTests adds the unit test module a Python file implies: a test file is its own
// module; helpers/x/y.py has the mirror tests/helpers/x/test_y.py, and a note when it is absent.
func (s *session) addUnitTests(rel string) {
	base := filepath.Base(rel)
	switch {
	case globMatch("tests/helpers/"+star+"/test_"+star+".py", rel) || globMatch("tests/helpers/test_"+star+".py", rel):
		s.plan.add("unit-tests", moduleName(rel), "")
	case globMatch("helpers/"+star+".py", rel):
		mirror := filepath.Join(filepath.Dir(filepath.Join("tests", rel)), "test_"+base)
		if regular, err := isRegular(filepath.Join(s.root, mirror)); err == nil && regular {
			s.plan.add("unit-tests", moduleName(mirror), "")
		} else if base != "__init__.py" {
			s.plan.notes = append(s.plan.notes, fmt.Sprintf("%s: no mirrored unit test (%s)", rel, mirror))
		}
	}
}

func moduleName(rel string) string {
	return strings.ReplaceAll(strings.TrimSuffix(rel, ".py"), "/", ".")
}
