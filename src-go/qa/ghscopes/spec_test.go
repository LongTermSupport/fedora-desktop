package ghscopes

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// findRaceWorld is a repository holding the find-race rule and fixture instead of gh-scopes'.
func findRaceWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	rules := strings.NewReplacer("gh-scope-outside-ssot", FindRace.ruleName,
		".semgrep/gh-scopes.*", ".semgrep/find-race.*").Replace(rulesText)
	all := map[string]string{
		ruleFile:             "",
		fixtureFile:          "",
		FindRace.ruleFile:    rules,
		FindRace.fixtureFile: "# fixture\n# ruleid: " + FindRace.ruleName + "\n",
	}
	maps.Copy(all, files)
	return newWorld(t, all)
}

func (w *world) mainSpec(s Spec, args ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := s.Main(args, Env{Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	return result{code, stdout.String(), stderr.String()}
}

// The find-race gate is the same engine over its own rule file, fixture and words.
func TestTheFindRaceSpecRunsItsOwnRuleAndFixture(t *testing.T) {
	w := findRaceWorld(t, nil)
	got := w.mainSpec(FindRace)
	wantExit(t, got, ExitOK)
	wantTest := []string{"semgrep", "--test", "--config", filepath.Join(w.root, ".semgrep/find-race.yml"), filepath.Join(w.root, ".semgrep/find-race.fixture")}
	if !w.run.called(wantTest...) {
		t.Fatalf("no self-test of the find-race rule: %v", w.run.calls)
	}
	if !slices.Contains(w.run.scanArgv, filepath.Join(w.root, ".semgrep/find-race.yml")) {
		t.Fatalf("the scan did not use the find-race rule: %v", w.run.scanArgv)
	}
}

func TestTheFindRaceUsageNamesItsCommand(t *testing.T) {
	w := findRaceWorld(t, nil)
	got := w.mainSpec(FindRace, "--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-find-race", ".semgrep/find-race.yml")
}

func TestAFindRaceFindingIsNamedAndCountedInItsOwnWords(t *testing.T) {
	w := findRaceWorld(t, map[string]string{"tasks/probe.yml": "x\n"})
	w.run.scan = func(files []string) string {
		return report(scannable(files), []hit{{"tasks/probe.yml", 56}}, nil)
	}
	got := w.mainSpec(FindRace)
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout,
		"tasks/probe.yml:56  find-race-negated-stat-test\n",
		"find-race-negated-stat-test: 1 line(s) in 1 file(s) run find -ignore_readdir_race with a negated stat test. See CLAUDE/QA.md \"find-race-negated-stat-test\".\n")
}

func TestAFindRaceRuleThatFailsItsFixtureNamesTheFindRaceFiles(t *testing.T) {
	w := findRaceWorld(t, nil)
	w.run.testCode = 1
	got := w.mainSpec(FindRace)
	wantExit(t, got, ExitFailed)
	wantIn(t, "stderr", got.stderr, "ERROR: find-race-negated-stat-test failed its fixture (.semgrep/find-race.fixture):")
}

func TestAFindRaceRuleWithoutExcludesNamesItsRuleFile(t *testing.T) {
	w := findRaceWorld(t, map[string]string{FindRace.ruleFile: "rules:\n  - id: x\n    languages: [generic]\n"})
	got := w.mainSpec(FindRace)
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: .semgrep/find-race.yml declares no paths.exclude list")
}

func TestAFindRaceSkipIsHeadedByItsOwnName(t *testing.T) {
	w := findRaceWorld(t, map[string]string{"big.bin": "x\n"})
	w.run.scan = func([]string) string {
		return report([]string{"a.txt"}, nil, []string{"big.bin"})
	}
	got := w.mainSpec(FindRace)
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "⚠ find-race: 1 file(s) skipped by semgrep")
}
