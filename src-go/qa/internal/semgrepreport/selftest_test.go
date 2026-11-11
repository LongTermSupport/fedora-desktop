package semgrepreport

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// fixtureText is a fixture in the shapes semgrep's test runner reads: a rule marked on a line of
// its own, two ids on one line, an ok line, a todoruleid the rule may miss, a todook it may hit,
// and the //, <!-- --> and (* *) comment forms.
const fixtureText = `import os
# ruleid: rule-a
os.walk(top)
# ruleid: rule-a, rule-b
both()
# ok: rule-a
fine()
# todoruleid: rule-a
missed_for_now()
# todook: rule-b
hit_for_now()
// ruleid: rule-c
js()
<!-- ruleid: rule-d -->
html()
(* ruleid:deepok:rule-e *)
ml()
`

func TestExpectedLinesPortsSemgrepsAnnotations(t *testing.T) {
	got := ExpectedLines(fixtureText)
	want := map[string][]int{
		"rule-a": {3, 5},
		"rule-b": {5},
		"rule-c": {13},
		"rule-d": {15},
		"rule-e": {17},
	}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// selfTestWorld is a fixture and a rule file on disk, and a fake semgrep that answers the scan.
type selfTestWorld struct {
	test  SelfTest
	argv  []string
	copy  string
	reply func(copyPath string) proc.Result
}

// rulesText is a rule file whose rules keep off the fixture by their paths, one through an
// anchor another rule aliases.
const rulesText = `rules:
  - id: rule-a
    paths:
      include: [scripts/test-*]
      exclude: &own ['.semgrep/rule.*']
    pattern: os.walk(...)
    languages: [python]
    message: m
    severity: ERROR
  - id: rule-b
    paths:
      exclude: *own
    pattern: both()
    languages: [python]
    message: m
    severity: ERROR
`

func newSelfTestWorld(t *testing.T, fixture string) *selfTestWorld {
	t.Helper()
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules.yml")
	fx := filepath.Join(dir, "rule.fixture.py")
	for path, text := range map[string]string{rules: rulesText, fx: fixture} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &selfTestWorld{test: SelfTest{Rules: rules, Fixture: fx, Name: ".semgrep/rule.fixture.py", TempDir: dir}}
}

func (w *selfTestWorld) prove(t *testing.T) error {
	t.Helper()
	return w.test.Prove(func(argv []string) (proc.Result, error) {
		w.argv = argv
		w.copy = argv[len(argv)-1]
		text, err := os.ReadFile(w.copy)
		if err != nil {
			t.Fatalf("the copy semgrep was handed cannot be read: %v", err)
		}
		original, err := os.ReadFile(w.test.Fixture)
		if err != nil {
			t.Fatal(err)
		}
		if string(text) != string(original) {
			t.Fatalf("the copy is not the fixture")
		}
		return w.reply(w.copy), nil
	})
}

// report is semgrep's JSON report with one result per rule and line, and the errors given. It is
// built from maps because semgrep's keys are snake_case.
func report(t *testing.T, path string, hits map[string][]int, errs ...string) proc.Result {
	t.Helper()
	results := []map[string]any{}
	for _, id := range slices.Sorted(maps.Keys(hits)) {
		for _, line := range hits[id] {
			results = append(results, map[string]any{"check_id": id, "path": path, "start": map[string]int{"line": line}})
		}
	}
	failures := make([]map[string]string, 0, len(errs))
	for _, e := range errs {
		failures = append(failures, map[string]string{"type": "Timeout", "path": path, "message": e})
	}
	doc := map[string]any{"results": results, "errors": failures, "paths": map[string][]string{"scanned": {path}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return proc.Result{Stdout: string(data)}
}

func expected() map[string][]int { return ExpectedLines(fixtureText) }

func TestSelfTestPassesWhenEveryMarkedLineIsReported(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result {
		hits := expected()
		// A todoruleid line the rule already hits, and a todook line it still hits.
		hits["rule-a"] = append(hits["rule-a"], 9)
		hits["rule-b"] = append(hits["rule-b"], 11)
		return report(t, p, hits)
	}
	if err := w.prove(t); err != nil {
		t.Fatal(err)
	}
}

// The scan is a plain one whose limit the gate sets: never semgrep's test runner, which runs
// every rule with a fixed 5 s.
func TestSelfTestScansWithTheGatesLimit(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result { return report(t, p, expected()) }
	if err := w.prove(t); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scan", TimeoutFlag, "--timeout-threshold=0", "--json", "--no-rewrite-rule-ids", "--scan-unknown-extensions", "--no-git-ignore", "--metrics=off", "--disable-version-check", "--config"} {
		if !slices.Contains(w.argv, want) {
			t.Errorf("argv lacks %s: %v", want, w.argv)
		}
	}
	if slices.Contains(w.argv, "--test") || slices.Contains(w.argv, "test") {
		t.Errorf("argv runs semgrep's test runner: %v", w.argv)
	}
}

// semgrep's test runner sets every rule's paths aside, so a rule whose include or exclude keeps
// it off its fixture is still proven against it; the self-test hands semgrep the rules without
// them, aliases resolved, every other key kept.
func TestSelfTestSetsTheRulesPathsAside(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	var handed string
	w.reply = func(p string) proc.Result {
		data, err := os.ReadFile(w.argv[slices.Index(w.argv, "--config")+1])
		if err != nil {
			t.Fatal(err)
		}
		handed = string(data)
		return report(t, p, expected())
	}
	if err := w.prove(t); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(handed, "paths") || strings.Contains(handed, "&own") {
		t.Fatalf("the rules semgrep was handed keep their paths:\n%s", handed)
	}
	for _, want := range []string{"id: rule-a", "id: rule-b", "pattern: os.walk(...)", "pattern: both()", "severity: ERROR"} {
		if !strings.Contains(handed, want) {
			t.Fatalf("the rules semgrep was handed lack %q:\n%s", want, handed)
		}
	}
	if slices.Contains(w.argv, w.test.Rules) {
		t.Fatalf("semgrep was handed the rule file itself: %v", w.argv)
	}
}

func TestSelfTestCannotRunOnRulesItCannotRead(t *testing.T) {
	for name, text := range map[string]string{"not YAML": "rules: [\n", "no rules": "other: 1\n", "a rule not a mapping": "rules: [x]\n"} {
		w := newSelfTestWorld(t, fixtureText)
		if err := os.WriteFile(w.test.Rules, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		err := w.test.Prove(func([]string) (proc.Result, error) {
			t.Fatal("semgrep ran")
			return proc.Result{}, nil
		})
		var fe *FixtureError
		if err == nil || errors.As(err, &fe) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// The fixture is scanned as a copy whose name no rule's paths exclude, the way semgrep's test
// runner ignores them, and the copy is gone afterwards.
func TestSelfTestScansARenamedCopy(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result { return report(t, p, expected()) }
	if err := w.prove(t); err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(w.copy); base != "selftest.py" {
		t.Errorf("copy named %s", base)
	}
	if strings.Contains(w.copy, "fixture") || filepath.Dir(w.copy) == filepath.Dir(w.test.Fixture) {
		t.Errorf("copy at %s is not apart from the fixture", w.copy)
	}
	if _, err := os.Stat(filepath.Dir(w.copy)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the copy's directory is left behind: %v", err)
	}
}

func failureOf(t *testing.T, err error) *FixtureError {
	t.Helper()
	var ff *FixtureError
	if !errors.As(err, &ff) {
		t.Fatalf("not a fixture failure: %v", err)
	}
	if strings.Contains(ff.Error(), "selftest") {
		t.Errorf("the message names the copy, not the fixture:\n%s", ff.Error())
	}
	return ff
}

func TestSelfTestFailsAMissedLine(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result {
		hits := expected()
		hits["rule-a"] = []int{3}
		return report(t, p, hits)
	}
	ff := failureOf(t, w.prove(t))
	if !strings.Contains(ff.Error(), "rule-a: missed lines [5]") || !strings.Contains(ff.Error(), ".semgrep/rule.fixture.py") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

func TestSelfTestFailsAnUnmarkedLine(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result {
		hits := expected()
		hits["rule-a"] = append(hits["rule-a"], 7)
		return report(t, p, hits)
	}
	if ff := failureOf(t, w.prove(t)); !strings.Contains(ff.Error(), "rule-a: unexpected lines [7]") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

// A rule that reports nothing while another still does is the starved taint rule of issue 206:
// it fails, and names the rule.
func TestSelfTestFailsARuleThatReportedNothing(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result {
		hits := expected()
		delete(hits, "rule-a")
		return report(t, p, hits)
	}
	if ff := failureOf(t, w.prove(t)); !strings.Contains(ff.Error(), "rule-a: missed lines [3 5]") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

// An id only an ok line names, which nothing reports while other rules do, is semgrep's rule id
// mismatch: the fixture names a rule the file does not have.
func TestSelfTestFailsAnIDNoRuleReports(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText+"# ok: rule-z\nother()\n")
	w.reply = func(p string) proc.Result { return report(t, p, expected()) }
	if ff := failureOf(t, w.prove(t)); !strings.Contains(ff.Error(), "rule-z") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

// Any error in the report fails, a Timeout above all: the gate's limit was passed, so the rule
// was not proven.
func TestSelfTestFailsATimeout(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result { return report(t, p, expected(), "rule-a timed out") }
	ff := failureOf(t, w.prove(t))
	if !strings.Contains(ff.Error(), "Timeout") || !strings.Contains(ff.Error(), "rule-a timed out") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

func TestSelfTestFailsAFileItDidNotScan(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(string) proc.Result { return report(t, "elsewhere.py", nil) }
	if ff := failureOf(t, w.prove(t)); !strings.Contains(ff.Error(), "did not scan") {
		t.Fatalf("message:\n%s", ff.Error())
	}
}

// A semgrep that gave no report could not run the test: that is not the fixture's failure.
func TestSelfTestCannotRunWithoutAReport(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(string) proc.Result { return proc.Result{Code: 2, Stderr: "bad config"} }
	err := w.prove(t)
	var ff *FixtureError
	if err == nil || errors.As(err, &ff) || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelfTestCannotRunOnAFailedExit(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.reply = func(p string) proc.Result {
		res := report(t, p, expected())
		res.Code = 7
		return res
	}
	var ff *FixtureError
	if err := w.prove(t); err == nil || errors.As(err, &ff) {
		t.Fatalf("err = %v", err)
	}
}

func TestSelfTestRefusesAFixtureThatMarksNothing(t *testing.T) {
	w := newSelfTestWorld(t, "fine()\n# ok: rule-a\nfine()\n")
	w.reply = func(p string) proc.Result { return report(t, p, nil) }
	err := w.prove(t)
	if err == nil || !strings.Contains(err.Error(), "marks no ruleid line") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelfTestCarriesTheRunnersError(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	boom := errors.New("cannot start semgrep")
	err := w.test.Prove(func([]string) (proc.Result, error) { return proc.Result{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSelfTestCannotReadAMissingFixture(t *testing.T) {
	w := newSelfTestWorld(t, fixtureText)
	w.test.Fixture = filepath.Join(t.TempDir(), "gone.py")
	if err := w.test.Prove(func([]string) (proc.Result, error) {
		t.Fatal("semgrep ran")
		return proc.Result{}, nil
	}); err == nil {
		t.Fatal("a missing fixture proved")
	}
}
