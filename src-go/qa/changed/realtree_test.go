package changed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// These tests read the repository's own tables: they are what catches a hand edit that leaves
// the data, the bash reader or the registry disagreeing.

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

func TestTheBashReaderAndTheGoReaderAgreeOnTheExcludedTrees(t *testing.T) {
	want, err := loadTrees(filepath.Join(realScripts, "qa-trees.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := filepath.Abs(filepath.Join(realScripts, "qa-discovery.bash"))
	if err != nil {
		t.Fatal(err)
	}
	script := `set -euo pipefail
source "$1"
printf '%s\n' "${QA_EXCLUDE_DIRS[@]}"
echo ---
printf '%s\n' "${QA_PY_EXCLUDE_DIRS[@]}"`
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"bash", "-c", script, "bash", discovery}})
	if err != nil || res.Code != 0 {
		t.Fatalf("bash reader: %v %+v", err, res)
	}
	shell, python, found := strings.Cut(strings.TrimSpace(res.Stdout), "\n---\n")
	if !found {
		t.Fatalf("unexpected output %q", res.Stdout)
	}
	if got := strings.Split(shell, "\n"); !reflect.DeepEqual(treeList(got), want.shell) {
		t.Errorf("shell list differs:\n bash %v\n go   %v", got, want.shell)
	}
	if got := strings.Split(python, "\n"); !reflect.DeepEqual(treeList(got), want.python) {
		t.Errorf("python list differs:\n bash %v\n go   %v", got, want.python)
	}
}

func TestTheBashReaderRefusesAMissingOrBrokenTable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "qa-discovery.bash", readReal(t, "qa-discovery.bash"))
	writeFile(t, dir, "lib/qa-trees.bash", readReal(t, "lib/qa-trees.bash"))
	run := func() proc.Result {
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"bash", "-c", "source \"$1\"", "bash", filepath.Join(dir, "qa-discovery.bash")}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run(); res.Code != 2 || !strings.Contains(res.Stderr, "cannot read") {
		t.Fatalf("a missing table: %+v", res)
	}
	writeFile(t, dir, "qa-trees.tsv", "ruby\tx\n")
	if res := run(); res.Code != 2 || !strings.Contains(res.Stderr, "unknown list 'ruby'") {
		t.Fatalf("an unknown list: %+v", res)
	}
	writeFile(t, dir, "qa-trees.tsv", readReal(t, "qa-trees.tsv"))
	if res := run(); res.Code != 0 {
		t.Fatalf("the real table: %+v", res)
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
	for _, want := range []string{"test-qa-changed.bash", "test-qa-helper-summary.bash"} {
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
	if !strings.Contains(readReal(t, "qa-bash.bash"), `select(.level == "error" or .level == "warning")`) {
		t.Errorf("qa-bash.bash no longer gates on error and warning; qa-changed's %s has drifted", shellcheckSeverity)
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
