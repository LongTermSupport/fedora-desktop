package semgrepreport

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// SelfTest proves a rule file against its annotated fixture with a plain scan whose limit the
// gate sets (TimeoutFlag), judged here against the fixture's annotations the way semgrep's test
// runner judges them, the rules' paths set aside as that runner sets them aside. It replaces semgrep --test, which scans every fixture with a fixed 5 s per
// rule per file that no flag raises, so a loaded host made a sound rule report nothing
// (fedora-desktop#206). Any error in the report, a Timeout included, fails the test.
type SelfTest struct {
	// Rules is the rule file's path.
	Rules string
	// Fixture is the fixture's path on disk.
	Fixture string
	// Name is the fixture as messages name it, its path in the repository.
	Name string
	// TempDir is where the fixture's copy is made ("" is the system's).
	TempDir string
}

// FixtureError is a self-test that ran and found the rules and the fixture disagree.
type FixtureError struct {
	Fixture  string
	Problems []string
}

func (f *FixtureError) Error() string {
	return fmt.Sprintf("%s failed its self-test:\n    %s", f.Fixture, strings.Join(f.Problems, "\n    "))
}

// Prove runs the scan through run and judges its report. A *FixtureError is the fixture's
// verdict; any other error is a test that could not run. run's own error is returned as it is:
// it is the gate's, which already names semgrep.
func (s SelfTest) Prove(run func(argv []string) (proc.Result, error)) (err error) {
	data, err := os.ReadFile(s.Fixture)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", s.Name, err)
	}
	marks := parseAnnotations(string(data))
	if len(marks.expected()) == 0 {
		return fmt.Errorf("%s marks no ruleid line; the self-test proves nothing", s.Name)
	}
	dir, err := os.MkdirTemp(s.TempDir, "semgrep-selftest-")
	if err != nil {
		return fmt.Errorf("cannot make a directory for the copy of %s: %w", s.Name, err)
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("cannot remove the copy of %s: %w", s.Name, rmErr))
		}
	}()
	// A rule's paths (include and exclude) would keep it off its own fixture, so, as semgrep's
	// test runner does, they are set aside: the rules go to semgrep without them, and the fixture
	// is scanned as a copy apart from the checkout.
	ruleText, err := os.ReadFile(s.Rules)
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return fmt.Errorf("cannot read the rules proven against %s: %w", s.Name, pe.Err)
	}
	if err != nil {
		return fmt.Errorf("cannot read the rules proven against %s: %w", s.Name, err)
	}
	rules, err := rulesWithoutPaths(ruleText)
	if err != nil {
		return fmt.Errorf("the rules proven against %s: %w", s.Name, err)
	}
	copyName := "selftest" + filepath.Ext(s.Fixture)
	for name, text := range map[string][]byte{selfTestRules: rules, copyName: data} {
		if writeErr := writeIn(dir, name, text); writeErr != nil {
			return fmt.Errorf("cannot write the self-test of %s: %w", s.Name, writeErr)
		}
	}
	copyPath := filepath.Join(dir, copyName)
	res, err := run(s.argv(filepath.Join(dir, selfTestRules), copyPath))
	if err != nil {
		return err
	}
	return s.judge(res, copyPath, marks)
}

// selfTestRules is the name of the rule file semgrep is handed.
const selfTestRules = "selftest-rules.yml"

// rulesWithoutPaths is the rule file data with every rule's paths taken out, its aliases
// resolved by decoding.
func rulesWithoutPaths(data []byte) ([]byte, error) {
	var doc struct {
		Rules []any `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("cannot be read: %w", err)
	}
	if len(doc.Rules) == 0 {
		return nil, errors.New("hold no rules")
	}
	for i, rule := range doc.Rules {
		fields, ok := rule.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rule %d is not a mapping", i+1)
		}
		delete(fields, "paths")
	}
	out, err := yaml.Marshal(map[string]any{"rules": doc.Rules})
	if err != nil {
		return nil, fmt.Errorf("cannot be written again: %w", err)
	}
	return out, nil
}

// writeIn writes data as name inside dir, and nowhere else.
func writeIn(dir, name string, data []byte) (err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	return root.WriteFile(name, data, 0o600)
}

// argv is the scan of the copy with the rules at rules.
func (s SelfTest) argv(rules, copyPath string) []string {
	return []string{
		"semgrep", "scan", "--metrics=off", "--disable-version-check", "--config", rules,
		"--no-rewrite-rule-ids", "--scan-unknown-extensions", "--no-git-ignore", "--json", "--quiet",
		TimeoutFlag, "--timeout-threshold=0", "--max-target-bytes=0", copyPath,
	}
}

// judge decides the self-test from semgrep's result.
func (s SelfTest) judge(res proc.Result, copyPath string, marks annotations) error {
	name := func(text string) string { return strings.ReplaceAll(text, copyPath, s.Name) }
	rep, err := Decode(res.Stdout)
	if err != nil {
		return fmt.Errorf("semgrep gave no report on %s (exit %d): %w\n%s", s.Name, res.Code, err, name(strings.TrimSpace(res.Stderr)))
	}
	if len(rep.Errors) > 0 {
		problems := make([]string, 0, len(rep.Errors))
		for _, e := range rep.Errors {
			problems = append(problems, name(fmt.Sprintf("semgrep error (%s): %s", e.Type, e.Message)))
		}
		return &FixtureError{Fixture: s.Name, Problems: problems}
	}
	if res.Code != 0 {
		return fmt.Errorf("semgrep exited %d on %s:\n%s", res.Code, s.Name, name(strings.TrimSpace(res.Stderr)))
	}
	if err := rep.Judge("fixture", []string{copyPath}); err != nil {
		return &FixtureError{Fixture: s.Name, Problems: []string{name(err.Error())}}
	}
	reported := lineSets{}
	for _, r := range rep.Results {
		if Tidy(r.Path) == Tidy(copyPath) {
			reported.add(r.CheckID, r.Start.Line)
		}
	}
	if problems := marks.compare(reported); len(problems) > 0 {
		return &FixtureError{Fixture: s.Name, Problems: problems}
	}
	return nil
}

// lineSets is lines by rule id.
type lineSets map[string]map[int]bool

func (l lineSets) add(id string, line int) {
	if l[id] == nil {
		l[id] = map[int]bool{}
	}
	l[id][line] = true
}

// lines is the lines of id, less those of every set in minus.
func (l lineSets) lines(id string, minus ...lineSets) []int {
	var out []int
	for line := range l[id] {
		if !slices.ContainsFunc(minus, func(m lineSets) bool { return m[id][line] }) {
			out = append(out, line)
		}
	}
	slices.Sort(out)
	return out
}

// annotations is what a fixture marks, as semgrep's test runner reads it: each annotation
// stands for the line after it.
type annotations struct {
	ruleid, ok, todoOK, todoRuleid lineSets
}

// commentStarts and commentEnds are the comment forms semgrep's test runner reads annotations in.
var (
	commentStarts = []string{"#", "//", "<!--", "(*"}
	commentEnds   = []string{"-->", "*)"}
)

// hasMarker is whether line holds word after a comment start, with or without a space.
func hasMarker(line, word string) bool {
	for _, start := range commentStarts {
		if strings.Contains(line, start+word) || strings.Contains(line, start+" "+word) {
			return true
		}
	}
	return false
}

// parseAnnotations reads a fixture's annotations as semgrep's test runner does (test.py).
func parseAnnotations(text string) annotations {
	a := annotations{ruleid: lineSets{}, ok: lineSets{}, todoOK: lineSets{}, todoRuleid: lineSets{}}
	for i, line := range strings.Split(text, "\n") {
		target := i + 2
		rule, ok := hasMarker(line, "ruleid"), hasMarker(line, "ok")
		todoRule, todoOK := hasMarker(line, "todoruleid"), hasMarker(line, "todook")
		if !rule && !ok && !todoRule && !todoOK {
			continue
		}
		ids, parsed := ruleIDs(line)
		if !parsed {
			continue
		}
		a.record(ids, target, map[*lineSets]bool{
			&a.ruleid:     rule || todoRule,
			&a.ok:         ok || todoRule,
			&a.todoOK:     todoOK,
			&a.todoRuleid: todoRule,
		})
	}
	return a
}

// record adds the ids at line to each set marked true, as test.py files a todoruleid line both
// as a ruleid and as an ok line.
func (a annotations) record(ids []string, line int, into map[*lineSets]bool) {
	for set, marked := range into {
		if !marked {
			continue
		}
		for _, id := range ids {
			set.add(id, line)
		}
	}
}

// ruleIDs is the ids an annotation line names: the text after its first colon, less a deepok,
// prook, deepruleid or proruleid prefix, split at commas, with a closing comment taken off. A
// line with no colon, or a prefixed line with more than one further colon, is not an annotation,
// as in semgrep.
func ruleIDs(line string) ([]string, bool) {
	halves := strings.SplitN(strings.TrimSpace(line), ":", 2)
	if len(halves) != 2 {
		return nil, false
	}
	text := strings.TrimSpace(halves[1])
	for _, prefix := range []string{"deepok", "prook", "deepruleid", "proruleid"} {
		if strings.HasPrefix(text, prefix) {
			parts := strings.Split(text, ":")
			if len(parts) != 2 {
				return nil, false
			}
			text = parts[1]
			break
		}
	}
	var ids []string
	for id := range strings.SplitSeq(text, ",") {
		id = strings.TrimSpace(id)
		for _, end := range commentEnds {
			id = strings.TrimSpace(strings.ReplaceAll(id, end, ""))
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, true
}

// expected is the lines each rule must report: its ruleid lines, less the todo ones.
func (a annotations) expected() map[string][]int {
	out := map[string][]int{}
	for id := range a.ruleid {
		if lines := a.ruleid.lines(id, a.todoRuleid, a.todoOK); len(lines) > 0 {
			out[id] = lines
		}
	}
	return out
}

// ExpectedLines is the lines each rule must report in a fixture, read as semgrep's test runner
// reads its annotations.
func ExpectedLines(fixture string) map[string][]int {
	return parseAnnotations(fixture).expected()
}

// compare is what the report and the annotations disagree on, as semgrep's test runner judges it:
// with anything reported, the rule ids the fixture names must be the ids reported, and each
// rule's reported lines must be its expected lines, todo lines set aside.
func (a annotations) compare(reported lineSets) []string {
	problems := a.mismatch(reported)
	ids := slices.Collect(maps.Keys(a.ruleid))
	for id := range reported {
		if a.ruleid[id] == nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		want := a.ruleid.lines(id, a.todoRuleid, a.todoOK)
		got := reported.lines(id, a.todoOK, a.todoRuleid)
		if missed := without(want, got); len(missed) > 0 {
			problems = append(problems, fmt.Sprintf("%s: missed lines %v", id, missed))
		}
		if extra := without(got, want); len(extra) > 0 {
			problems = append(problems, fmt.Sprintf("%s: unexpected lines %v", id, extra))
		}
	}
	return problems
}

// mismatch is test.py's rule id check: with anything reported, the ids the fixture's ruleid and
// ok lines name must be the ids reported.
func (a annotations) mismatch(reported lineSets) []string {
	if len(reported) == 0 {
		return nil
	}
	named := maps.Clone(a.ruleid)
	maps.Copy(named, a.ok)
	var silent, unnamed []string
	for id := range named {
		if reported[id] == nil {
			silent = append(silent, id)
		}
	}
	for id := range reported {
		if named[id] == nil {
			unnamed = append(unnamed, id)
		}
	}
	if len(silent)+len(unnamed) == 0 {
		return nil
	}
	slices.Sort(silent)
	slices.Sort(unnamed)
	return []string{fmt.Sprintf("rule id mismatch: named but reporting nothing %v, reporting but named nowhere %v", silent, unnamed)}
}

// without is the lines of a not in b.
func without(a, b []int) []int {
	var out []int
	for _, n := range a {
		if !slices.Contains(b, n) {
			out = append(out, n)
		}
	}
	return out
}
