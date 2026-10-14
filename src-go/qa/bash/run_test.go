package bash

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s lacks %q:\n%s", what, want, text)
		}
	}
}

func expect(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, code, got.stdout, got.stderr)
	}
}

// S1
func TestHelpPrintsTheUsageToStdout(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--help")
	expect(t, got, ExitOK)
	mustContain(t, "stdout", got.stdout, "usage: qa-bash", "--root", "--scripts")
	if len(w.run.calls) != 0 {
		t.Fatalf("help started processes: %v", w.run.calls)
	}
}

// S1
func TestBadArgumentsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--nope"}, {"--root"}, {"--scripts"}} {
		w := newWorld(t, nil)
		got := w.main(args...)
		expect(t, got, ExitCannotRun)
		mustContain(t, "stderr", got.stderr, "ERROR: ", "usage: qa-bash")
		w.noReport()
	}
}

// S1: the root is absolute and clean however it was given, and --scripts gives the root.
func TestTheRootIsMadeAbsoluteAndClean(t *testing.T) {
	for name, args := range map[string]func(w *world) []string{
		"dot":            func(*world) []string { return []string{"--root", "."} },
		"trailing slash": func(w *world) []string { return []string{"--root", w.root + "/"} },
		"scripts":        func(w *world) []string { return []string{"--scripts", filepath.Join(w.root, "scripts") + "/"} },
		"relative scripts": func(*world) []string {
			return []string{"--scripts", "scripts"}
		},
		"none": func(*world) []string { return nil },
	} {
		w := newWorld(t, nil)
		expect(t, w.main(args(w)...), ExitOK)
		for _, c := range w.run.calls {
			if c.Dir != w.root {
				t.Fatalf("%s: %v ran in %q, want %q", name, c.Argv, c.Dir, w.root)
			}
		}
	}
}

// S2
func TestTheReportPathDefaultsWhenTheVariableIsEmpty(t *testing.T) {
	w := newWorld(t, nil)
	w.env["QA_JSON_OUT"] = ""
	expect(t, w.main(), ExitOK)
	if !slices.Equal(w.wrote, []string{"/tmp/qa-bash-results.json"}) {
		t.Fatalf("wrote %v", w.wrote)
	}
}

// S2
func TestAnUnwritableReportIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stdout, stderr bytes.Buffer
	env := w.env0(&stdout, &stderr)
	env.WriteFile = func(string, []byte) error { return errors.New("read-only file system") }
	w.env["QA_JSON_OUT"] = "/nowhere/r.json"
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", stderr.String(), "ERROR: cannot write the report /nowhere/r.json: read-only file system")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("bad file descriptor") }

// S3
func TestAFailedWriteToStdoutIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stderr bytes.Buffer
	env := w.env0(nil, &stderr)
	env.Stdout = failingWriter{}
	w.env["QA_JSON_OUT"] = w.out
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	mustContain(t, "stderr", stderr.String(), "ERROR: cannot write to stdout")
}

// S4
func TestAMissingShellcheckIsExitTwoAndNamesThePlay(t *testing.T) {
	w := newWorld(t, nil)
	w.run.shellcheckMissing = true
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: shellcheck not installed", "playbooks/imports/play-devtools.yml", "by hand")
	w.noReport()
}

// S4
func TestShellcheckThatDoesNotAnswerAsShellcheckDoesIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.shellcheckVersion = "something else\n"
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: shellcheck --version")
	w.noReport()
}

// S4
func TestAMissingBashIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.bashMissing = true
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: bash not installed")
	w.noReport()
}

// S5: the shell list comes from the shared discovery, and its failure stops the run.
func TestADiscoveryFailureIsExitTwoAndNothingIsChecked(t *testing.T) {
	w := newWorld(t, nil)
	w.run.gitFails = 128
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: discovery of shell failed:")
	w.noReport()
	if len(w.run.cmds("shellcheck")) > 1 || len(w.run.cmds("bash")) > 1 {
		t.Fatalf("a check ran: %v", w.run.calls)
	}
}

// S5: no shell file at all is a broken gate, not a clean repository.
func TestNoShellFilesIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"tool.bash": ""})
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: discovery of shell failed:")
	w.noReport()
}

// S5: a script outside the trees table's exclusions is checked, one inside is not, whatever its
// name; a shebang makes an extensionless file a shell script.
func TestTheDiscoveredFilesAreTheOnesChecked(t *testing.T) {
	w := newWorld(t, map[string]string{
		"bin/prog":         "#!/usr/bin/env bash\necho\n",
		"untracked/x.bash": "echo SYNTAX_ERROR\n",
		"notes.txt":        "text\n",
	})
	expect(t, w.main(), ExitOK)
	var checked []string
	for _, c := range w.run.cmds("bash") {
		if c.Argv[1] == "-n" {
			checked = append(checked, c.Argv[3])
		}
	}
	if want := []string{"bin/prog", "tool.bash"}; !slices.Equal(checked, want) {
		t.Fatalf("checked %v, want %v", checked, want)
	}
}

// S6
func TestATrackedScriptNotPublishedIsExitTwoAndListed(t *testing.T) {
	w := newWorld(t, nil)
	w.write("gone.bash", "echo\n")
	w.run.files = []string{"scripts/qa-trees.tsv", "tool.bash"}
	w.run.tracked = []string{"scripts/qa-trees.tsv", "tool.bash", "gone.bash"}
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: discovery missed 1 tracked shell script(s):", "    gone.bash")
	w.noReport()
}

// S7
func TestASyntaxErrorFailsTheFileWithAMessageRelativeToTheRoot(t *testing.T) {
	w := newWorld(t, map[string]string{"lib/bad.bash": "if SYNTAX_ERROR\n"})
	got := w.main()
	expect(t, got, ExitFailed)
	mustContain(t, "stdout", got.stdout, "✗ bash: lib/bad.bash: lib/bad.bash: line 2: syntax error")
	if strings.Contains(got.stdout+w.reportText(), w.root) {
		t.Fatalf("the checkout's path is in the output:\n%s", got.stdout)
	}
	cmd := w.run.cmds("bash")[1]
	if !slices.Equal(cmd.Argv, []string{"bash", "-n", "--", "lib/bad.bash"}) || cmd.Dir != w.root || !cmd.Merge {
		t.Fatalf("bash -n was run as %+v", cmd)
	}
}

// S7: a status other than a syntax error is a script bash could not run, not a failing script.
func TestAnUnreadableScriptIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, map[string]string{"lib/odd.bash": "UNREADABLE\n"})
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: ", "lib/odd.bash", "127")
	w.noReport()
}

func (w *world) reportText() string {
	data, err := os.ReadFile(w.out)
	if err != nil {
		return ""
	}
	return string(data)
}

// S8, S9: shellcheck is given every file in one process, with no rc file and no environment
// options, following sources, from the root.
func TestShellcheckIsInvokedWithItsRulesPinned(t *testing.T) {
	w := newWorld(t, map[string]string{"b/two.bash": "echo\n"})
	expect(t, w.main(), ExitOK)
	calls := w.run.cmds("shellcheck")
	if len(calls) != 2 {
		t.Fatalf("shellcheck ran %d times: %v", len(calls), calls)
	}
	want := []string{"shellcheck", "--norc", "--external-sources", "--format", "json", "--", "b/two.bash", "tool.bash"}
	if !slices.Equal(calls[1].Argv, want) {
		t.Fatalf("argv %v, want %v", calls[1].Argv, want)
	}
	if calls[1].Dir != w.root || !slices.Contains(calls[1].Env, "SHELLCHECK_OPTS=") {
		t.Fatalf("shellcheck ran in %q with env %v", calls[1].Dir, calls[1].Env)
	}
}

// S10
func TestShellcheckStatusAndOutputAreChecked(t *testing.T) {
	cases := map[string]proc.Result{
		"status two":     {Code: 2, Stderr: "boom\n"},
		"not json":       {Stdout: "oops\n"},
		"empty output":   {Stdout: ""},
		"not an array":   {Stdout: "{}\n"},
		"killed":         {Code: -1},
		"foreign file":   lintAnswer(finding("elsewhere.bash", 1, "error", 1000, "x")),
		"unknown level":  lintAnswer(finding("tool.bash", 1, "fatal", 1000, "x")),
		"missing fields": {Stdout: `[{"file":"tool.bash"}]` + "\n", Code: 1},
	}
	for name, answer := range cases {
		w := newWorld(t, nil)
		w.run.lint = func([]string) proc.Result { return answer }
		got := w.main()
		if got.code != ExitCannotRun || !strings.HasPrefix(got.stderr, "ERROR: ") {
			t.Fatalf("%s: exit %d\nstderr:\n%s", name, got.code, got.stderr)
		}
		w.noReport()
	}
	w := newWorld(t, nil)
	w.run.lint = func([]string) proc.Result { return proc.Result{Code: 2, Stderr: "boom\n"} }
	mustContain(t, "stderr", w.main().stderr, "ERROR: shellcheck invocation failed (status 2):", "boom")
}

// S8a: the one shellcheck process has fifteen minutes for all files, and a timeout is named as one.
func TestAShellcheckTimeoutNamesTheLimit(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = func([]string) proc.Result { return proc.Result{TimedOut: true, Code: -1} }
	got := w.main()
	expect(t, got, ExitCannotRun)
	if !strings.Contains(got.stderr, "ERROR: shellcheck did not finish within 15m0s") {
		t.Fatalf("stderr %q", got.stderr)
	}
	w.noReport()
}

// S8a: each bash -n has one minute.
func TestABashSyntaxTimeoutNamesTheFileAndTheLimit(t *testing.T) {
	w := newWorld(t, nil)
	w.run.syntaxTimesOut = true
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: bash -n tool.bash: bash did not finish within 1m0s")
	w.noReport()
}

// S10: stdout that decodes without error but is not an array of findings is refused: null decodes
// to an empty list, so the leading bracket check is not redundant.
func TestShellcheckNullOutputIsNotAnArray(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = func([]string) proc.Result { return proc.Result{Stdout: "null\n"} }
	got := w.main()
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "ERROR: shellcheck did not print a JSON array of findings")
	w.noReport()
}

// S10: a path under the root is made relative, and every field survives.
func TestDiagnosticsKeepTheirFieldsAndGetRelativePaths(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = func([]string) proc.Result {
		d := finding(filepath.Join(w.root, "tool.bash"), 3, "info", 2016, "single quotes")
		d["fix"] = map[string]any{"replacements": []any{}}
		return lintAnswer(d)
	}
	expect(t, w.main(), ExitOK)
	diags := w.report().Diagnostics
	if len(diags) != 1 || diags[0]["file"] != "tool.bash" || diags[0]["code"] != float64(2016) || diags[0]["fix"] == nil || diags[0]["endColumn"] != float64(2) {
		t.Fatalf("diagnostics %v", diags)
	}
}

// S11, S13: info and style are an advisory; warning and error gate.
func TestOnlyErrorsAndWarningsGate(t *testing.T) {
	w := newWorld(t, nil)
	w.run.lint = func([]string) proc.Result {
		return lintAnswer(finding("tool.bash", 2, "info", 2016, "single quotes"), finding("tool.bash", 4, "style", 2250, "braces"))
	}
	got := w.main()
	expect(t, got, ExitOK)
	want := "⚠ shellcheck: 2 issues (see /tmp/qa-results.json .checks.bash.shellcheck_diagnostics)\n✓ bash: 1 files OK\n"
	if got.stdout != want {
		t.Fatalf("stdout %q, want %q", got.stdout, want)
	}
	if rep := w.report(); rep.Status != "pass" || len(rep.Diagnostics) != 2 || len(rep.Failures) != 0 {
		t.Fatalf("report %+v", rep)
	}
}

// S2, S13: the merged results path named in the advisory is QA_RESULTS_JSON when set.
func TestTheAdvisoryNamesTheMergedReport(t *testing.T) {
	w := newWorld(t, nil)
	w.env["QA_RESULTS_JSON"] = "/var/merged.json"
	w.run.lint = func([]string) proc.Result { return lintAnswer(finding("tool.bash", 2, "info", 2016, "q")) }
	mustContain(t, "stdout", w.main().stdout, "(see /var/merged.json .checks.bash.shellcheck_diagnostics)")
}

// S12, S13, S14, S15: a gating finding fails its file, with the code in the error and the file in
// failures; a file that fails both ways is one failure; the lines are sorted.
func TestGatingFindingsFailTheirFilesAndTheReportSaysWhich(t *testing.T) {
	w := newWorld(t, map[string]string{"a.bash": "echo SYNTAX_ERROR\n", "c.bash": "echo\n"})
	w.run.lint = func([]string) proc.Result {
		return lintAnswer(
			finding("tool.bash", 9, "warning", 2155, "Declare and assign separately"),
			finding("a.bash", 7, "error", 1072, "Unexpected"),
			finding("a.bash", 3, "warning", 2086, "Double quote"),
			finding("a.bash", 3, "info", 2016, "ignored for gating"),
		)
	}
	got := w.main()
	expect(t, got, ExitFailed)
	wantOut := "✗ bash: a.bash: a.bash: line 2: syntax error near unexpected token `fi'\n" +
		"⚠ shellcheck: 4 issues (see /tmp/qa-results.json .checks.bash.shellcheck_diagnostics)\n" +
		"✗ shellcheck: 3 error/warning-level finding(s) — gating\n" +
		"    a.bash:3 SC2086 (warning) Double quote\n" +
		"    a.bash:7 SC1072 (error) Unexpected\n" +
		"    tool.bash:9 SC2155 (warning) Declare and assign separately\n" +
		"✗ bash: 2/3 files failed → " + w.out + "\n"
	if got.stdout != wantOut {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got.stdout, wantOut)
	}
	rep := w.report()
	if rep.Type != "bash" || rep.Status != "fail" || rep.Summary != (summary{Total: 3, Passed: 1, Failed: 2}) {
		t.Fatalf("report %+v", rep)
	}
	files := make([]string, 0, len(rep.Results))
	for _, r := range rep.Results {
		files = append(files, r.File+":"+r.Type+":"+r.Status)
	}
	if want := []string{"a.bash:bash:fail", "c.bash:bash:pass", "tool.bash:bash:fail"}; !slices.Equal(files, want) {
		t.Fatalf("results %v", files)
	}
	wantErr := "a.bash: line 2: syntax error near unexpected token `fi'; SC2086:3 (warning) Double quote; SC1072:7 (error) Unexpected"
	if len(rep.Failures) != 2 || rep.Failures[0].Error != wantErr || rep.Failures[1].Error != "SC2155:9 (warning) Declare and assign separately" {
		t.Fatalf("failures %+v", rep.Failures)
	}
}

// S14, S15
func TestACleanRunPassesAndReportsEveryFile(t *testing.T) {
	w := newWorld(t, map[string]string{"x.sh": "#!/bin/sh\n:\n"})
	got := w.main()
	expect(t, got, ExitOK)
	if got.stdout != "✓ bash: 2 files OK\n" || got.stderr != "" {
		t.Fatalf("stdout %q stderr %q", got.stdout, got.stderr)
	}
	rep := w.report()
	if rep.Status != "pass" || rep.Summary != (summary{Total: 2, Passed: 2}) || len(rep.Results) != 2 || len(rep.Failures) != 0 || rep.Diagnostics == nil {
		t.Fatalf("report %+v", rep)
	}
	data := w.reportText()
	mustContain(t, "report", data, "\n  \"results\": [", "\"failures\": []", "\"shellcheck_diagnostics\": []")
}
