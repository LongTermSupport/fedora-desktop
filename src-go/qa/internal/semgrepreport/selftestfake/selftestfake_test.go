package selftestfake

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

const fixture = "# ruleid: rule-a\nbad()\n# ok: rule-a\nfine()\n"

func selfTest(t *testing.T) semgrepreport.SelfTest {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rule.py")
	rules := filepath.Join(dir, "rules.yml")
	for p, text := range map[string]string{path: fixture, rules: "rules:\n  - id: rule-a\n"} {
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return semgrepreport.SelfTest{Rules: rules, Fixture: path, Name: ".semgrep/rule.py", TempDir: dir}
}

func TestIsSelfTest(t *testing.T) {
	if !IsSelfTest([]string{"semgrep", "scan", "--json", "x"}) {
		t.Error("a scan argv is not the self-test")
	}
	for _, argv := range [][]string{{"semgrep", "--version"}, {"semgrep", "--config", "r", "x"}, {"git", "scan"}, {"semgrep"}} {
		if IsSelfTest(argv) {
			t.Errorf("%v taken for the self-test", argv)
		}
	}
}

func TestPassingPassesTheJudge(t *testing.T) {
	if err := selfTest(t).Prove(Passing); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFailsTheJudge(t *testing.T) {
	err := selfTest(t).Prove(Missing)
	var fe *semgrepreport.FixtureError
	if !errors.As(err, &fe) || !strings.Contains(err.Error(), "rule-a: missed lines [2]") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUnreadableCopyIsAnError(t *testing.T) {
	for _, answer := range []func([]string) (proc.Result, error){Passing, Missing} {
		if _, err := answer([]string{"semgrep", "scan", filepath.Join(t.TempDir(), "gone.py")}); err == nil {
			t.Error("a missing copy answered")
		}
	}
}
