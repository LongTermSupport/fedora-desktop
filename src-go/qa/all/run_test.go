package all

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAllGatesPassingPrintsTheStageLinesInTableOrderAndTheVerdict(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	want := []string{
		"✓ toolchain: TOOLCHAIN-OK 4 tools",
		"✓ deployed-drift: 3 scripts in sync",
		"✓ helper-tests: Ran 10 tests in 2 modules (2 tracked), 1 skipped",
		"✓ suite-a: passed: 1",
		"✓ suite-b: passed: 1",
		"✓ manifest: ONE 7; TWO 8",
		"✓ compat: All ok",
		"✓ QA passed: 8 files checked",
	}
	if got := stdoutLines(w.out.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("stdout:\n%s", strings.Join(got, "\n"))
	}
	if w.errOut.String() != "" {
		t.Fatalf("stderr %q", w.errOut.String())
	}
	wantRan := "qa-toolchain.bash qa-bash.bash qa-docs.bash qa-deployed-drift.bash qa-helper-tests.bash test-a.bash test-b.bash qa-manifest.bash python3 -m helpers.gnome.check_extension_compat"
	if got := strings.Join(w.runner.log, " "); got != wantRan {
		t.Fatalf("ran: %s", got)
	}
}

// The property the bash suite pinned by counting `exit 1` in the source: a failing gate does not
// disable the gates after it. Here every gate fails in turn and every other still runs.
func TestEveryGateRunsEvenWhenAnyOneOfThemFails(t *testing.T) {
	all := []string{"qa-toolchain.bash", "qa-bash.bash", "qa-docs.bash", "qa-deployed-drift.bash", "qa-helper-tests.bash", "test-a.bash", "test-b.bash", "qa-manifest.bash", "python3 -m helpers.gnome.check_extension_compat"}
	for _, failing := range all {
		t.Run(failing, func(t *testing.T) {
			w := newWorld(t, smallTable)
			passing(w)
			w.runner.replies[failing] = reply{stdout: "boom\n", code: 1, report: bashStage}
			if code := w.main(context.Background()); code != ExitFailed {
				t.Fatalf("exit %d", code)
			}
			if !reflect.DeepEqual(w.runner.log, all) {
				t.Fatalf("ran %v: a failure disabled the gates after it", w.runner.log)
			}
			if !strings.Contains(w.out.String(), "✗ QA FAILED: ") {
				t.Fatalf("no verdict:\n%s", w.out.String())
			}
		})
	}
}

func TestTheVerdictNamesEveryFailedGateAndStageInRunOrder(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-a.bash"] = reply{code: 1}
	w.runner.replies["qa-manifest.bash"] = reply{code: 2}
	w.runner.replies["qa-docs.bash"] = reply{code: 1, report: bashStage}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	tail := stdoutLines(w.out.String())
	want := []string{
		"✗ QA FAILED: 1 errors in 7 files",
		"  2 gate(s) failed: suite-a manifest",
		"  1 stage(s) failed: docs",
		"  Details: jq '.failures[]' " + w.env["QA_RESULTS_JSON"],
	}
	if got := tail[len(tail)-4:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("verdict:\n%s", strings.Join(got, "\n"))
	}
}

func TestTheReportCarriesTheMergedStagesAndStatus(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-bash.bash"] = reply{code: 1, report: bashStage}
	w.main(context.Background())
	report := w.results()
	for _, want := range []string{`"status": "fail"`, `"total": 7`, `"failed": 1`, `"bash": {`, `"docs": {`, "a<b>.sh"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	// The status is the exit codes', not the numbers': a failed hard gate alone makes it fail.
	w2 := newWorld(t, smallTable)
	passing(w2)
	w2.runner.replies["test-a.bash"] = reply{code: 1}
	w2.main(context.Background())
	if !strings.Contains(w2.results(), `"status": "fail"`) || !strings.Contains(w2.results(), `"failed": 0`) {
		t.Fatalf("report:\n%s", w2.results())
	}
}

func TestAnInterruptEndsTheRunWithoutAVerdictOrAReport(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	ctx, cancel := context.WithCancel(context.Background())
	w.runner.replies["test-a.bash"] = reply{onRun: cancel, code: -1}
	if code := w.main(ctx); code != ExitInterrupted {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasSuffix(w.out.String(), "✗ qa-all interrupted\n") || strings.Contains(w.out.String(), "suite-a") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	if got := w.runner.log[len(w.runner.log)-1]; got != "test-a.bash" {
		t.Fatalf("ran %s after the interrupt", got)
	}
	if _, err := os.Stat(w.env["QA_RESULTS_JSON"]); err == nil {
		t.Fatal("an interrupted run wrote a report")
	}
}

func TestHelpIsOnStdoutAndABadCommandLineIsExitTwoOnStderr(t *testing.T) {
	w := newWorld(t, smallTable)
	if code := w.main(context.Background(), "--help"); code != ExitOK || !strings.HasPrefix(w.out.String(), "usage: qa-all") || w.errOut.String() != "" {
		t.Fatalf("--help: exit %d, stdout %q, stderr %q", code, w.out.String(), w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("help ran gates: %v", w.runner.log)
	}
	if code := w.main(context.Background(), "--typo"); code != ExitCannotRun || w.out.String() != "" ||
		!strings.Contains(w.errOut.String(), "ERROR: unknown argument '--typo'") || !strings.Contains(w.errOut.String(), "usage: qa-all") {
		t.Fatalf("--typo: exit %d, stdout %q, stderr %q", code, w.out.String(), w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("a usage error ran gates: %v", w.runner.log)
	}
}

func TestARunThatCannotStartSaysWhyAndRunsNothing(t *testing.T) {
	w := newWorld(t, smallTable)
	w.env["QA_ALL_TIMEOUT"] = "soon"
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "QA_ALL_TIMEOUT") {
		t.Fatalf("bad timeout: exit %d, stderr %q", code, w.errOut.String())
	}
	delete(w.env, "QA_ALL_TIMEOUT")
	if err := os.Remove(filepath.Join(w.root, "scripts/test-b.bash")); err != nil {
		t.Fatal(err)
	}
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "test-b.bash") {
		t.Fatalf("missing script: exit %d, stderr %q", code, w.errOut.String())
	}
	if err := os.Remove(filepath.Join(w.root, "scripts/qa-gates.tsv")); err != nil {
		t.Fatal(err)
	}
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "qa-gates.tsv") {
		t.Fatalf("missing table: exit %d, stderr %q", code, w.errOut.String())
	}
	if code := w.main(context.Background(), "--root", filepath.Join(w.root, "nowhere")); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "is not a directory") {
		t.Fatalf("bad root: exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("ran %v", w.runner.log)
	}
}

func TestAReportThatCannotBeClearedOrWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	// A non-empty directory cannot be removed as a stale report.
	dir := filepath.Join(w.root, "occupied")
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.env["QA_RESULTS_JSON"] = dir
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "cannot clear the previous report") {
		t.Fatalf("clear: exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("ran gates with a report path it could not use: %v", w.runner.log)
	}
	w.env["QA_RESULTS_JSON"] = filepath.Join(w.root, "no", "such", "dir", "r.json")
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "cannot write the report") {
		t.Fatalf("write: exit %d, stderr %q", code, w.errOut.String())
	}
}

func TestTheReportPathDefaultsToTheOneCIUploads(t *testing.T) {
	if got := reportPath(func(string) string { return "" }); got != "/tmp/qa-results.json" {
		t.Fatalf("got %q", got)
	}
	if got := reportPath(func(string) string { return "/x/r.json" }); got != "/x/r.json" {
		t.Fatalf("got %q", got)
	}
}
