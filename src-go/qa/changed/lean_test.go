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

// suiteSkipWorld is a run world whose test-one.bash drives the teams command, built from
// src-go/internal/teams, with a store for recorded passes.
func suiteSkipWorld(t *testing.T) (*runWorld, string) {
	t.Helper()
	w := newRunWorld(t)
	writeFile(t, w.scripts, "go-binary-suites.tsv", "teams\tscripts/test-one.bash\n")
	w.runner.replies["go list -deps"] = proc.Result{Stdout: filepath.Join(w.root, "src-go/internal/teams") + "\n"}
	writeFile(t, w.root, "src-go/internal/teams/t.go", "package teams\n")
	w.env["QA_SUITE_PASSES"] = filepath.Join(t.TempDir(), "passes")
	return w, filepath.Join(w.scripts, "test-one.bash")
}

// suiteRunsFrom counts the runs of the script among the calls from the index on.
func suiteRunsFrom(calls []proc.Cmd, from int, script string) int {
	n := 0
	for _, c := range calls[from:] {
		if len(c.Argv) > 1 && c.Argv[1] == script {
			n++
		}
	}
	return n
}

// A suite picked only because its command's build closure changed is skipped on a later local
// run over the same inputs after it passed, and runs again once an input changes.
func TestLocallyASuiteWhoseInputsPassedBeforeIsSkipped(t *testing.T) {
	w, script := suiteSkipWorld(t)
	if code := w.main(); code != ExitOK {
		t.Fatalf("first run: exit %d, stderr %q", code, w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, 0, script); n != 1 {
		t.Fatalf("first run ran the suite %d times", n)
	}
	from := len(w.runner.calls)
	if code := w.main(); code != ExitOK {
		t.Fatalf("second run: exit %d, stderr %q", code, w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, from, script); n != 0 {
		t.Fatalf("second run over the same inputs ran the suite %d times", n)
	}
	if want := "✓ suite test-one.bash: skipped, unchanged since a recorded pass (builds from src-go/internal/teams/t.go); CI runs it\n"; !strings.Contains(w.out.String(), want) {
		t.Fatalf("stdout lacks %q:\n%s", want, w.out.String())
	}
	writeFile(t, w.root, "src-go/internal/teams/t.go", "package teams\n\nvar x = 1\n")
	from = len(w.runner.calls)
	if code := w.main(); code != ExitOK {
		t.Fatalf("third run: exit %d, stderr %q", code, w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, from, script); n != 1 {
		t.Fatalf("a changed input must run the suite again; ran %d times", n)
	}
}

// A failed suite records nothing, so it runs again.
func TestAFailedSuiteIsNotRecorded(t *testing.T) {
	w, script := suiteSkipWorld(t)
	w.runner.replies["bash "+script] = proc.Result{Stdout: "passed: 1 failed: 1\n", Code: 1}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	w.runner.replies["bash "+script] = proc.Result{Stdout: "passed: 1 failed: 0\n"}
	from := len(w.runner.calls)
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, from, script); n != 1 {
		t.Fatalf("a failed suite was skipped (ran %d times)", n)
	}
}

// With nowhere to keep passes the run says so and runs every suite; a pass that cannot be
// written is reported and costs only a rerun.
func TestNoStoreOrAnUnwritableOneRunsTheSuiteAndSaysSo(t *testing.T) {
	w, script := suiteSkipWorld(t)
	delete(w.env, "QA_SUITE_PASSES")
	for range 2 {
		if code := w.main(); code != ExitOK {
			t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
		}
		if !strings.Contains(w.errOut.String(), "no suite pass is remembered") {
			t.Fatalf("stderr %q", w.errOut.String())
		}
	}
	if n := suiteRunsFrom(w.runner.calls, 0, script); n != 2 {
		t.Fatalf("with no store the suite ran %d times, want 2", n)
	}
	blocker := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, filepath.Dir(blocker), "a-file", "x")
	w.env["QA_SUITE_PASSES"] = filepath.Join(blocker, "passes")
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if !strings.Contains(w.errOut.String(), "suite scripts/test-one.bash runs: cannot read the suite pass record") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, 0, script); n != 3 {
		t.Fatalf("with an unreadable store the suite must run; ran %d times in all, want 3", n)
	}
}

// In CI a recorded pass is never read: every picked suite runs.
func TestInCIARecordedPassIsIgnored(t *testing.T) {
	w, script := suiteSkipWorld(t)
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	w.env["CI"] = "true"
	from := len(w.runner.calls)
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if n := suiteRunsFrom(w.runner.calls, from, script); n != 1 {
		t.Fatalf("CI skipped a suite (ran %d times)", n)
	}
}

// A suite picked for any other reason too (it names or reaches a changed file, or changed
// itself) always runs: the key covers only the command's build closure and the suite.
func TestASuitePickedForAnotherReasonAlwaysRuns(t *testing.T) {
	w, script := suiteSkipWorld(t)
	w.changeKept()
	for range 2 {
		from := len(w.runner.calls)
		if code := w.main(); code != ExitOK {
			t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
		}
		if n := suiteRunsFrom(w.runner.calls, from, script); n != 1 {
			t.Fatalf("a suite that names a changed file was skipped (ran %d times)", n)
		}
	}
}
