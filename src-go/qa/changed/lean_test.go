package changed

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// argvOf is the argv of the first call to the script, or nil.
func argvOf(calls []proc.Cmd, script string) []string {
	for _, c := range calls {
		if len(c.Argv) > 1 && c.Argv[0] == toolBash && c.Argv[1] == script {
			return c.Argv
		}
	}
	return nil
}

// Locally, a semgrep gate a changed file asks for runs over the changed files only: its
// wrapper is given them after --, and the report says so.
func TestLocallyASemgrepGateRunsOverTheChangedFilesOnly(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	script := filepath.Join(w.scripts, "qa-argv-leak.bash")
	if got := argvOf(w.runner.calls, script); !slices.Equal(got, []string{toolBash, script, "--", "kept.bash"}) {
		t.Fatalf("argv-leak ran as %v", got)
	}
	if want := "✓ argv-leak: secret-in-argv rules, 1 changed file(s) (CI runs it whole)\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
}

// In CI the same gate runs whole, as it always has.
func TestInCIASemgrepGateRunsWhole(t *testing.T) {
	w := newRunWorld(t)
	w.env["CI"] = "true"
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	script := filepath.Join(w.scripts, "qa-argv-leak.bash")
	if got := argvOf(w.runner.calls, script); !slices.Equal(got, []string{toolBash, script}) {
		t.Fatalf("argv-leak ran as %v", got)
	}
	if want := "✓ argv-leak: secret-in-argv rules, whole gate\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
}

// CI set to false or 0 is not CI.
func TestCIFalseOrZeroIsLocal(t *testing.T) {
	for _, v := range []string{"", "false", "0"} {
		if ciEnabled(func(string) string { return v }) {
			t.Fatalf("CI=%q read as CI", v)
		}
	}
	if !ciEnabled(func(string) string { return "1" }) {
		t.Fatal("CI=1 not read as CI")
	}
}

// A change to what the gate is made of (its rules and fixtures, its tool, its wrapper, the
// wrapper library or the trees table) runs it whole even locally: the rule must be re-applied
// to every file.
func TestAChangeToTheGateItselfRunsItWholeLocally(t *testing.T) {
	for _, source := range []string{".semgrep/argv-leak.yml", "src-go/qa/ghscopes/run.go", "scripts/qa-argv-leak.bash", "scripts/lib/go-tool.bash", "scripts/qa-trees.tsv"} {
		if !gateSource("qa-argv-leak.bash", source) {
			t.Errorf("%s is not seen as part of the gate", source)
		}
	}
	for _, other := range []string{"kept.bash", "scripts/qa-gh-poll.bash", "files/x.service", "src-go/internal/x/x.go"} {
		if gateSource("qa-argv-leak.bash", other) {
			t.Errorf("%s is seen as part of the gate", other)
		}
	}
}

// End to end: a changed rule file runs the gate whole locally.
func TestLocallyAChangedRuleRunsTheGateWhole(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	writeFile(t, w.root, ".semgrep/argv-leak.yml", "rules: []\n")
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	script := filepath.Join(w.scripts, "qa-argv-leak.bash")
	if got := argvOf(w.runner.calls, script); !slices.Equal(got, []string{toolBash, script}) {
		t.Fatalf("argv-leak ran as %v", got)
	}
}
