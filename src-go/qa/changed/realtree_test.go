package changed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// These tests read the repository's own tables: they are what catches a hand edit that leaves
// the data or the registry disagreeing.

func TestTheRealRoutingTableLoads(t *testing.T) {
	r, err := loadRouting(filepath.Join(realScripts, "qa-routing.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range r.perFileGates {
		if _, err := os.Stat(filepath.Join(realScripts, gate+".bash")); err != nil {
			t.Errorf("per-file gate %s is not a script: %v", gate, err)
		}
	}
}

func TestEveryDeployedCommandHasASuiteRowAndEverySuiteIsRunByQAAll(t *testing.T) {
	r, err := loadRouting(filepath.Join(realScripts, "qa-routing.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	population, err := loadPopulation(realScripts, r.perFileGates)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := loadGoRows(filepath.Join(realScripts, "go-binary-suites.tsv"), population)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, row := range rows {
		have[row.command] = true
	}
	commands, err := os.ReadDir("../../cmd")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commands {
		if c.IsDir() && !have[c.Name()] {
			t.Errorf("src-go/cmd/%s is built and deployed but has no row in scripts/go-binary-suites.tsv", c.Name())
		}
	}
}

func TestThePopulationHoldsTheSuitesAFreshReaderWouldExpect(t *testing.T) {
	r, err := loadRouting(filepath.Join(realScripts, "qa-routing.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	population, err := loadPopulation(realScripts, r.perFileGates)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"test-qa-changed.bash", "test-qa-docs-exit-codes.bash"} {
		found := false
		for _, name := range population {
			found = found || name == want
		}
		if !found {
			t.Errorf("%s missing from the population %v", want, population)
		}
	}
	for _, name := range population {
		for _, gate := range r.perFileGates {
			if name == gate+".bash" {
				t.Errorf("per-file gate %s is in the population", gate)
			}
		}
	}
}

func TestRoutingAndShellcheckArgumentsMatchTheGatesTheyDuplicate(t *testing.T) {
	gate, err := os.ReadFile(filepath.Join(realScripts, "..", "src-go", "qa", "bash", "shellcheck.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gate), `d.level == "error" || d.level == "warning"`) {
		t.Errorf("the shell gate no longer gates on error and warning; qa-changed's %s has drifted", shellcheckSeverity)
	}
	if !strings.Contains(string(gate), `"--external-sources"`) {
		t.Errorf("the shell gate no longer follows sources; qa-changed's %s has drifted", shellcheckFollow)
	}
	if !strings.Contains(readReal(t, "qa-patterns.bash"), ".semgrep/bash-conventions.yml") {
		t.Error("qa-patterns.bash no longer uses .semgrep/bash-conventions.yml; qa-changed's copy has drifted")
	}
}

// qa-changed treats a suite that exits 0 without a "passed: <n>" line as having proved
// nothing, so a suite it can select must print one. This reads each suite's source (it runs
// nothing) for a printed "passed: <counter>" line.
func TestEverySelectableSuitePrintsTheCanonicalCountLine(t *testing.T) {
	r, err := loadRouting(filepath.Join(realScripts, "qa-routing.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	population, err := loadPopulation(realScripts, r.perFileGates)
	if err != nil {
		t.Fatal(err)
	}
	printsCount := regexp.MustCompile(`passed:[[:space:]]*(%[ds]|[$][{(]?[A-Za-z_])`)
	for _, name := range population {
		if !strings.HasPrefix(name, "test-") {
			continue
		}
		text, err := os.ReadFile(filepath.Join(realScripts, name))
		if err != nil {
			t.Fatal(err)
		}
		if !printsCount.Match(text) {
			t.Errorf("%s can be selected by qa-changed but prints no 'passed: <n> failed: <m>' line, so qa-changed would call it a suite that proved nothing", name)
		}
	}
}

// Every file under .semgrep/ is read by the gate or suite listed here; a change to one must run
// it, and none may be called covered when nothing checked it (SPEC-qa-changed B33).
var semgrepReaders = map[string]string{
	".semgrep/bash-conventions.yml":  "check:patterns",
	".semgrep/bash-conventions.bash": "check:patterns",
	".semgrep/gh-scopes.yml":         "check:gh-scopes",
	".semgrep/gh-scopes.fixture":     "check:gh-scopes",
	".semgrep/ready-wait.yml":        "check:ready-wait",
	".semgrep/ready-wait.py":         "check:ready-wait",
	".semgrep/ready-wait.bash":       "check:ready-wait",
	".semgrep/speech-to-text.yml":    "scripts/qa-speech-to-text-rules.bash",
	".semgrep/speech-to-text.js":     "scripts/qa-speech-to-text-rules.bash",
	".semgrep/speech-to-text.py":     "scripts/qa-speech-to-text-rules.bash",
}

func TestEverySemgrepFileRoutesToTheGateThatReadsIt(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range publishedUnder(t, root, ".semgrep") {
		if _, ok := semgrepReaders[rel]; !ok {
			t.Errorf("%s is in the tree but has no reader in this test: name the gate that reads it, or delete the file", rel)
		}
	}
	for rel, reader := range semgrepReaders {
		checkSemgrepRoute(t, root, rel, reader)
	}
}

// publishedUnder lists the files under dir that git would publish (tracked, or untracked and not
// ignored), the scope every gate uses, so a gitignored __pycache__ is not "in the tree".
func publishedUnder(t *testing.T, root, dir string) []string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", dir},
		Dir:  root,
	})
	if err != nil || res.Code != 0 {
		t.Fatalf("git ls-files under %s: %v %+v", dir, err, res)
	}
	var out []string
	for f := range strings.SplitSeq(res.Stdout, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func TestPublishedUnderSkipsIgnoredFilesButCatchesNewUntrackedOnes(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: repo})
		if err != nil || res.Code != 0 {
			t.Fatalf("git %v: %v %+v", args, err, res)
		}
	}
	git("init", "-q")
	writeFile(t, repo, ".gitignore", "__pycache__/\n")
	writeFile(t, repo, ".semgrep/tracked.yml", "x")
	git("add", ".gitignore", ".semgrep/tracked.yml")
	writeFile(t, repo, ".semgrep/__pycache__/x.pyc", "x")
	writeFile(t, repo, ".semgrep/new-untracked.yml", "x")
	got := publishedUnder(t, repo, ".semgrep")
	slices.Sort(got)
	want := []string{".semgrep/new-untracked.yml", ".semgrep/tracked.yml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("published files %v, want %v", got, want)
	}
}

func checkSemgrepRoute(t *testing.T, root, rel, reader string) {
	t.Helper()
	{
		s := &session{
			env:     Env{Runner: &scriptedRunner{replies: map[string]proc.Result{}}, Stderr: &strings.Builder{}, Stdout: &strings.Builder{}},
			root:    root,
			scripts: filepath.Join(root, "scripts"),
			opts:    options{base: "HEAD"},
		}
		if err := s.load(); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if err := s.classify(ctx, collected{existing: []string{rel}}); err != nil {
			t.Fatal(err)
		}
		if err := s.selectSuites(ctx); err != nil {
			t.Fatal(err)
		}
		p := s.plan
		if id, isCheck := strings.CutPrefix(reader, "check:"); isCheck {
			if !p.has(id) {
				t.Errorf("%s does not run the %s check: %v", rel, id, keys(p))
			}
		} else if !slices.ContainsFunc(p.suites, func(ps pickedSuite) bool { return ps.name == reader }) {
			t.Errorf("%s does not run %s: suites %+v", rel, reader, p.suites)
		}
		if len(p.uncovered) != 0 || len(p.skipped) != 0 {
			t.Errorf("%s: uncovered %v skipped %v", rel, p.uncovered, p.skipped)
		}
		if !p.covered[rel] {
			t.Errorf("%s is not covered by the check or suite that reads it", rel)
		}
	}
}

// Every active plan script of the tree, and the gate's own files, run the plan-script-logging
// check against the repository's own routing table. The scripts are read from the tree, so a plan
// script this test has not met is still routed.
func TestEveryRealActivePlanScriptRunsThePlanScriptLoggingCheck(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	active := regexp.MustCompile(`^CLAUDE/Plan/[0-9]+-[^/]+/.*\.(bash|sh)$`)
	inputs := []string{"scripts/qa-plan-script-logging.bash", "src-go/qa/planscriptlogging/scan.go", "src-go/qa/cmd/qa-plan-script-logging/main.go"}
	for _, rel := range publishedUnder(t, root, "CLAUDE/Plan") {
		if active.MatchString(rel) {
			inputs = append(inputs, rel)
		}
	}
	if len(inputs) < 10 {
		t.Fatalf("found only %d inputs; the tree walk matched nothing: %v", len(inputs), inputs)
	}
	s := &session{
		env:     Env{Runner: &scriptedRunner{replies: map[string]proc.Result{}}, Stderr: &strings.Builder{}, Stdout: &strings.Builder{}},
		root:    root,
		scripts: filepath.Join(root, "scripts"),
		opts:    options{base: "HEAD"},
	}
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range inputs {
		s.plan = &plan{checks: map[string]*checkPlan{}, covered: map[string]bool{}}
		if err := s.classify(context.Background(), collected{existing: []string{rel}}); err != nil {
			t.Fatal(err)
		}
		if !s.plan.has("plan-script-logging") {
			t.Errorf("%s does not run the plan-script-logging check: %v", rel, keys(s.plan))
		}
	}
}
