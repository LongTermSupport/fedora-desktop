package ghscopes

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport/selftestfake"
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
	if !w.run.selfTested(filepath.Join(w.root, ".semgrep/find-race.yml"), ".fixture") {
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
	w.run.test = selftestfake.Missing
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

// archPinWorld is a repository holding the arch-pin rule and fixture instead of gh-scopes'.
func archPinWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	rules := strings.NewReplacer("gh-scope-outside-ssot", ArchPin.ruleName,
		".semgrep/gh-scopes.*", ".semgrep/arch-pin.*").Replace(rulesText)
	all := map[string]string{
		ruleFile:            "",
		fixtureFile:         "",
		ArchPin.ruleFile:    rules,
		ArchPin.fixtureFile: "# fixture\n# ruleid: " + ArchPin.ruleName + "\n",
	}
	maps.Copy(all, files)
	return newWorld(t, all)
}

// The arch-pin gate is the same engine over its own rule file, fixture and words.
func TestTheArchPinSpecRunsItsOwnRuleAndFixture(t *testing.T) {
	w := archPinWorld(t, nil)
	got := w.mainSpec(ArchPin)
	wantExit(t, got, ExitOK)
	if !w.run.selfTested(filepath.Join(w.root, ".semgrep/arch-pin.yml"), ".fixture") {
		t.Fatalf("no self-test of the arch-pin rule: %v", w.run.calls)
	}
	if !slices.Contains(w.run.scanArgv, filepath.Join(w.root, ".semgrep/arch-pin.yml")) {
		t.Fatalf("the scan did not use the arch-pin rule: %v", w.run.scanArgv)
	}
}

func TestTheArchPinUsageNamesItsCommand(t *testing.T) {
	w := archPinWorld(t, nil)
	got := w.mainSpec(ArchPin, "--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-arch-pin", ".semgrep/arch-pin.yml")
}

func TestAnArchPinFindingIsNamedAndCountedInItsOwnWords(t *testing.T) {
	w := archPinWorld(t, map[string]string{"Dockerfile": "x\n"})
	w.run.scan = func(files []string) string {
		return report(scannable(files), []hit{{"Dockerfile", 7}}, nil)
	}
	got := w.mainSpec(ArchPin)
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout,
		"Dockerfile:7  arch-pinned-artefact\n",
		"arch-pinned-artefact: 1 line(s) in 1 file(s) fix a download, package or image to one CPU architecture. See CLAUDE/QA.md \"arch-pinned-artefact\".\n")
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

// argvLeakWorld is a repository holding the secret-in-argv rule and fixture instead of
// gh-scopes'.
func argvLeakWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	rules := strings.NewReplacer("gh-scope-outside-ssot", ArgvLeak.ruleName,
		".semgrep/gh-scopes.*", ".semgrep/argv-leak.*").Replace(rulesText)
	all := map[string]string{
		ruleFile:             "",
		fixtureFile:          "",
		ArgvLeak.ruleFile:    rules,
		ArgvLeak.fixtureFile: "# fixture\n# ruleid: " + ArgvLeak.ruleName + "\n",
	}
	maps.Copy(all, files)
	return newWorld(t, all)
}

// The secret-in-argv gate is the same engine over its own rule file, fixture and words.
func TestTheArgvLeakSpecRunsItsOwnRuleAndFixture(t *testing.T) {
	w := argvLeakWorld(t, nil)
	got := w.mainSpec(ArgvLeak)
	wantExit(t, got, ExitOK)
	if !w.run.selfTested(filepath.Join(w.root, ".semgrep/argv-leak.yml"), ".yml") {
		t.Fatalf("no self-test of the secret-in-argv rule: %v", w.run.calls)
	}
	if !slices.Contains(w.run.scanArgv, filepath.Join(w.root, ".semgrep/argv-leak.yml")) {
		t.Fatalf("the scan did not use the secret-in-argv rule: %v", w.run.scanArgv)
	}
}

func TestTheArgvLeakUsageNamesItsCommand(t *testing.T) {
	w := argvLeakWorld(t, nil)
	got := w.mainSpec(ArgvLeak, "--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-argv-leak", ".semgrep/argv-leak.yml")
}

func TestAArgvLeakFindingIsNamedAndCountedInItsOwnWords(t *testing.T) {
	w := argvLeakWorld(t, map[string]string{"play.yml": "x\n"})
	w.run.scan = func(files []string) string {
		return report(scannable(files), []hit{{"play.yml", 7}}, nil)
	}
	got := w.mainSpec(ArgvLeak)
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout,
		"play.yml:7  secret-in-argv\n",
		"secret-in-argv: 1 line(s) in 1 file(s) put a secret on a command line. See CLAUDE/QA.md \"secret-in-argv\".\n")
}

// secretScreenWorld is a repository holding the secret-on-normal-screen rule and its three
// fixtures instead of gh-scopes'.
func secretScreenWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	rules := strings.NewReplacer("gh-scope-outside-ssot", SecretScreen.ruleName,
		".semgrep/gh-scopes.*", ".semgrep/secret-screen.*").Replace(rulesText)
	all := map[string]string{
		ruleFile:                 "",
		fixtureFile:              "",
		SecretScreen.ruleFile:    rules,
		SecretScreen.fixtureFile: "# fixture\n# ruleid: " + SecretScreen.ruleName + "\n",
	}
	for _, more := range SecretScreen.moreFixtures {
		all[more] = "# fixture\n# ruleid: " + SecretScreen.ruleName + "\n"
	}
	maps.Copy(all, files)
	return newWorld(t, all)
}

// A rule file written for several languages is proven against a fixture in each: the shell
// fixture, the Python one and the Ansible one, every run.
func TestTheSecretScreenSpecProvesEveryFixture(t *testing.T) {
	w := secretScreenWorld(t, nil)
	got := w.mainSpec(SecretScreen)
	wantExit(t, got, ExitOK)
	rules := filepath.Join(w.root, ".semgrep/secret-screen.yml")
	for _, ext := range []string{".bash", ".py", ".yml"} {
		if !w.run.selfTested(rules, ext) {
			t.Fatalf("no self-test of the secret-screen rules against its %s fixture: %v", ext, w.run.calls)
		}
	}
	if !slices.Contains(w.run.scanArgv, rules) {
		t.Fatalf("the scan did not use the secret-screen rules: %v", w.run.scanArgv)
	}
}

// A further fixture that is missing is a gate that cannot prove its rules: exit 2, nothing
// scanned.
func TestAMissingFurtherFixtureIsExitTwo(t *testing.T) {
	w := secretScreenWorld(t, map[string]string{".semgrep/secret-screen.py": ""})
	got := w.mainSpec(SecretScreen)
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: cannot read .semgrep/secret-screen.py")
	if w.run.scanArgv != nil {
		t.Fatalf("scanned with a fixture missing: %v", w.run.scanArgv)
	}
}

// A further fixture that marks no ruleid line proves nothing: exit 2.
func TestAFurtherFixtureMarkingNothingIsExitTwo(t *testing.T) {
	w := secretScreenWorld(t, map[string]string{".semgrep/secret-screen.fixture.yml": "# nothing marked\n"})
	got := w.mainSpec(SecretScreen)
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, ".semgrep/secret-screen.fixture.yml marks no ruleid line")
}

func TestASecretScreenFindingIsNamedAndCountedInItsOwnWords(t *testing.T) {
	w := secretScreenWorld(t, map[string]string{"vault.bash": "x\n"})
	w.run.scan = func(files []string) string {
		return report(scannable(files), []hit{{"vault.bash", 85}}, nil)
	}
	got := w.mainSpec(SecretScreen)
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout,
		"vault.bash:85  secret-on-normal-screen\n",
		"secret-on-normal-screen: 1 line(s) in 1 file(s) write a secret to the terminal's normal screen. See CLAUDE/QA.md \"secret-on-normal-screen\".\n")
}

func TestTheSecretScreenUsageNamesItsCommand(t *testing.T) {
	w := secretScreenWorld(t, nil)
	got := w.mainSpec(SecretScreen, "--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-secret-screen", ".semgrep/secret-screen.yml")
}
