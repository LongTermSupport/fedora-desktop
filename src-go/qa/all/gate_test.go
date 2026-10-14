package all

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

const smallTable = `gate	toolchain	scripts/qa-toolchain.bash	QA tool versions do not match	detail:TOOLCHAIN-OK .+
stage	bash	scripts/qa-bash.bash	ERROR: Missing required tools. Install them and re-run.
stage	docs	scripts/qa-docs.bash	ERROR: docs gate could not produce a result
drift	deployed-drift	scripts/qa-deployed-drift.bash	repo-owned scripts differ
helpers	helper-tests	scripts/qa-helper-tests.bash	the helper unit suite failed
gate	suite-a	scripts/test-a.bash	suite a failed	count
gate	suite-b	scripts/test-b.bash	suite b failed	count
gate	manifest	scripts/qa-manifest.bash	manifest invalid	detail:ONE [0-9]+	detail:TWO [0-9]+
gate	compat	python3 -m helpers.gnome.check_extension_compat	extension drift	detail:All ok
`

// passing makes every gate of smallTable pass with a summary its readers can read.
func passing(w *world) {
	w.runner.replies["qa-toolchain.bash"] = reply{stdout: "TOOLCHAIN-OK 4 tools\n"}
	w.runner.replies["qa-deployed-drift.bash"] = reply{stdout: "✓ deployed-drift: 3 scripts in sync\n"}
	w.runner.replies["qa-manifest.bash"] = reply{stdout: "ONE 7\nTWO 8\n"}
	w.runner.replies["python3 -m helpers.gnome.check_extension_compat"] = reply{stdout: "All ok\n"}
}

func TestAHardGatePassesWithTheSummaryItsReadersFind(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-a.bash"] = reply{stdout: "case\npassed: 12  failed: 0\n"}
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	out := w.out.String()
	for _, want := range []string{
		"✓ toolchain: TOOLCHAIN-OK 4 tools\n",
		"✓ suite-a: passed: 12\n",
		"✓ suite-b: passed: 1\n",
		"✓ manifest: ONE 7; TWO 8\n",
		"✓ compat: All ok\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAFailedHardGateShowsItsCaptureOnStderrAndNoPassLine(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-a.bash"] = reply{stdout: "case 1 ok\n", stderr: "case 2 BAD\n\n\n", code: 1}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if got := w.errOut.String(); got != "case 1 ok\ncase 2 BAD\n" {
		t.Fatalf("stderr %q: the capture, trailing newlines trimmed, one added", got)
	}
	out := w.out.String()
	if !strings.Contains(out, "✗ suite-a: suite a failed\n") || strings.Contains(out, "✓ suite-a") {
		t.Fatalf("stdout:\n%s", out)
	}
}

// Bug B2: a suite that exits 0 without a count proved nothing.
func TestACountGateWithNoCountFails(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["test-b.bash"] = reply{stdout: "Summary: 3 passed, 0 failed\n"}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ suite-b: exited 0 but printed no 'passed: <n>' line, so it proved nothing\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	if !strings.Contains(w.errOut.String(), "Summary: 3 passed") {
		t.Fatalf("the capture belongs on stderr: %q", w.errOut.String())
	}
}

// Bug B3: a pattern that stops matching is a failed gate, not "summary unreadable".
func TestADetailGateWhoseSummaryIsNotFoundFails(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-manifest.bash"] = reply{stdout: "ONE 7\n"}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ manifest: ran, but its summary line was not found: TWO [0-9]+\n") || strings.Contains(w.out.String(), "unreadable") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}

func TestAGateThatCannotStartIsAFailureNamingTheReason(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["python3 -m helpers.gnome.check_extension_compat"] = reply{err: errors.New("exec: python3: not found")}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "python3: not found") || !strings.Contains(w.out.String(), "✗ compat: extension drift") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", w.out.String(), w.errOut.String())
	}
}

// Bug B8: a gate that outlives its limit is killed and reported with the limit.
func TestAGateThatOutlivesItsTimeoutIsAFailureNamingTheLimit(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.env["QA_ALL_TIMEOUT"] = "90s"
	w.runner.replies["test-a.bash"] = reply{stdout: "partial\n", timedOut: true, code: -1}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ suite-a: suite a failed (timed out after 1m30s)\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}

// Bug B11: every gate runs with the repository root as its directory.
func TestEveryGateRunsFromTheRepositoryRoot(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.main(context.Background())
	if len(w.runner.dirs) != 9 {
		t.Fatalf("ran %d gates", len(w.runner.dirs))
	}
	for i, dir := range w.runner.dirs {
		if dir != w.root {
			t.Errorf("gate %d (%s) ran in %q, not %q", i, w.runner.log[i], dir, w.root)
		}
	}
}

func TestTheDriftGateSpeaksForItselfAndMustSpeak(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-deployed-drift.bash"] = reply{stdout: "⚠ deployed-drift: skipped (CCY container)\n\n"}
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("a skip is not a failure: exit %d, stderr %q", code, w.errOut.String())
	}
	if !strings.Contains(w.out.String(), "⚠ deployed-drift: skipped (CCY container)\n✓ helper-tests") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	// Bug B12: a pass that printed no stage line is indistinguishable from a gate that stopped.
	w.runner.replies["qa-deployed-drift.bash"] = reply{}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ deployed-drift: ran, but printed no deployed-drift stage line\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	w.runner.replies["qa-deployed-drift.bash"] = reply{stderr: "drift in ccy\n", code: 1}
	if code := w.main(context.Background()); code != ExitFailed || !strings.Contains(w.out.String(), "✗ deployed-drift: repo-owned scripts differ\n") {
		t.Fatalf("exit %d, stdout:\n%s", code, w.out.String())
	}
}

func TestTheHelperGateReadsItsCountsFromTheFileItWasHandedTheTokenFor(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if code := w.main(context.Background()); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if !strings.Contains(w.out.String(), "✓ helper-tests: Ran 10 tests in 2 modules (2 tracked), 1 skipped\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}

func TestTheHelperGateFailsOnceForEachWayItCanFail(t *testing.T) {
	cases := []struct {
		name     string
		reply    reply
		stdout   string
		stderr   string
		inCapure string
	}{
		{"the suite failed", reply{stdout: "out\n", stderr: "err\n", code: 1}, "✗ helper-tests: the helper unit suite failed\n", "out\nerr\n", ""},
		{"it wrote to stdout", reply{stdout: "forged line\n"}, "✗ helper-tests: wrote to stdout, which is this suite's verdict stream\n", "A test printing here can forge or split a stage line.", "forged line"},
		{"the counts are unreadable", reply{counts: "token=%TOKEN%\ntests=x\nskipped=0\nmodules=1\ntracked=1\n"}, "✗ helper-tests: ran, but its counts could not be read\n", "helper-tests counts: tests= is missing or not a number", ""},
		{"the counts carry another run's token", reply{counts: "token=other\ntests=1\nskipped=0\nmodules=1\ntracked=1\n"}, "✗ helper-tests: ran, but its counts could not be read\n", "carries token", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, smallTable)
			passing(w)
			w.runner.replies["qa-helper-tests.bash"] = c.reply
			if code := w.main(context.Background()); code != ExitFailed {
				t.Fatalf("exit %d", code)
			}
			if strings.Count(w.out.String(), "helper-tests:") != 1 || !strings.Contains(w.out.String(), c.stdout) {
				t.Fatalf("stdout:\n%s", w.out.String())
			}
			if !strings.Contains(w.errOut.String(), c.stderr) || !strings.Contains(w.errOut.String(), c.inCapure) {
				t.Fatalf("stderr:\n%s", w.errOut.String())
			}
		})
	}
}

func TestAStagePassesItsOutputThroughAndItsFailureNeedsNoLineOfOurs(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-bash.bash"] = reply{stdout: "✗ bash: 1/3 files failed\n", stderr: "detail\n", code: 1, report: bashStage}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if strings.Count(w.out.String(), "bash:") != 1 || !strings.Contains(w.errOut.String(), "detail\n") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", w.out.String(), w.errOut.String())
	}
	if !strings.Contains(w.out.String(), "✗ QA FAILED: 1 errors in 7 files\n  1 stage(s) failed: bash\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}

// A stage that exits 2 cannot run its tools: the one abort. No later gate runs, no report is
// written, and the report of an earlier run is gone (bug B6).
func TestAStageThatCannotRunItsToolsAbortsTheRunAndLeavesNoStaleReport(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if err := os.WriteFile(w.env["QA_RESULTS_JSON"], []byte(`{"status":"pass"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w.runner.replies["qa-bash.bash"] = reply{stderr: "no shellcheck\n", code: 2}
	if code := w.main(context.Background()); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasSuffix(w.errOut.String(), "no shellcheck\nERROR: Missing required tools. Install them and re-run.\n") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
	if got := strings.Join(w.runner.log, " "); got != "qa-toolchain.bash qa-bash.bash" {
		t.Fatalf("ran %s: nothing after the abort", got)
	}
	if _, err := os.Stat(w.env["QA_RESULTS_JSON"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the earlier run's report survived: %v", err)
	}
	if strings.Contains(w.out.String(), "QA passed") || strings.Contains(w.out.String(), "QA FAILED") {
		t.Fatalf("an abort has no verdict:\n%s", w.out.String())
	}
}

// Bug B4: a stage that wrote no usable report is that stage's failure, in its own slot.
func TestAStageWithNoUsableReportFailsInItsOwnSlot(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-bash.bash"] = reply{report: "{"}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ bash: its report is unusable: the report is not a JSON report object") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	if !strings.Contains(w.results(), `"bash": null`) || !strings.Contains(w.results(), `"docs": {`) {
		t.Fatalf("report:\n%s", w.results())
	}
}

// Bug B5: exit 0 with a report counting failures is not a pass.
func TestAStageThatExitsZeroWhileCountingFailuresFails(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-docs.bash"] = reply{report: bashStage}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ docs: exited 0 but its report counts 1 failed\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}

func TestAStageThatOutlivesItsTimeoutFailsAndNeedsNoReport(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.runner.replies["qa-docs.bash"] = reply{timedOut: true, code: -1, report: "{"}
	if code := w.main(context.Background()); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	out := w.out.String()
	if !strings.Contains(out, "✗ docs: timed out after 30m0s\n") || strings.Contains(out, "report is unusable") {
		t.Fatalf("stdout:\n%s", out)
	}
	w.runner.replies["qa-docs.bash"] = reply{err: errors.New("exec: bash: not found")}
	w.main(context.Background())
	if !strings.Contains(w.out.String(), "✗ docs: could not start: cannot run") && !strings.Contains(w.out.String(), "✗ docs: could not start: exec: bash: not found") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
}
