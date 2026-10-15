package sttrules

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// S1
func TestHelpIsUsageOnStdoutExitZero(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, Main([]string{"-h"}, w.env()))
	w.wantStdout("usage: qa-speech-to-text-rules")
	if w.stderr.Len() != 0 {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// S1
func TestUsageErrorsAreExitTwoWithTheUsage(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--root"}, {"--scripts"}} {
		w := newWorld(t, nil)
		w.wantExit(ExitCannotRun, Main(args, w.env()))
		w.wantStderr("ERROR: ", "usage: qa-speech-to-text-rules")
	}
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--bogus"}, w.env()))
	w.wantStderr("ERROR: unknown argument '--bogus'")
}

// S1
func TestScriptsFlagSetsTheRoot(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, Main([]string{"--scripts", filepath.Join(w.root, "scripts")}, w.env()))
}

// S2
func TestAStdoutThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main([]string{"--root", w.root}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("ERROR: cannot write to stdout: closed")
}

// S2: nothing but the payload reaches stdout.
func TestStdoutHoldsOnlyThePayload(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	if w.stdout.String() != "passed: 2 failed: 0\n" || w.stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.stdout.String(), w.stderr.String())
	}
}

// S3
func TestSymlinkedRootNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, map[string]string{"helpers/mod.py": "x\n"})
	w.fake.scan = func(_ string, files []string) proc.Result {
		return reportFor(t, files, []jsonResult{hit("helpers/mod.py", 1)}, nil)
	}
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitFailed, Main([]string{"--root", link}, w.env()))
	if all := w.stdout.String() + w.stderr.String(); strings.Contains(all, w.root) || strings.Contains(all, link) {
		t.Fatalf("a checkout path leaked:\n%s", all)
	}
	for _, dir := range w.fake.dirs {
		if dir != w.root {
			t.Fatalf("semgrep ran in %q, want the real root %q", dir, w.root)
		}
	}
}

// S3
func TestAnUnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--root", filepath.Join(w.root, "missing")}, w.env()))
	w.wantStderr("ERROR: cannot resolve the repository root")
}

// S4
func TestSemgrepThatCannotRunIsExitTwoSayingWhyAndNothingElseRuns(t *testing.T) {
	cases := map[string]struct {
		set  func(*fakeRunner)
		want string
	}{
		"missing":   {func(f *fakeRunner) { f.versionEr = exec.ErrNotFound }, "executable file not found"},
		"exits 1":   {func(f *fakeRunner) { f.version = proc.Result{Code: 1, Stderr: "broken install\n"} }, "semgrep --version exited 1: broken install"},
		"timed out": {func(f *fakeRunner) { f.version = proc.Result{TimedOut: true} }, "semgrep did not finish within"},
		"leftover":  {func(f *fakeRunner) { f.version = proc.Result{Leftover: true} }, "semgrep left a process holding its output open"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{ruleFile: ""})
			c.set(w.fake)
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: semgrep not found. Install with: pipx install semgrep (", c.want)
			if len(w.fake.calls) != 1 {
				t.Fatalf("something ran after the failed --version: %v", w.fake.calls)
			}
		})
	}
}

// S5
func TestAMissingOrIrregularRuleOrFixtureIsExitTwoNamingIt(t *testing.T) {
	for _, name := range []string{ruleFile, jsFixtureFile, pyFixtureFile} {
		w := newWorld(t, map[string]string{name: ""})
		w.wantExit(ExitCannotRun, w.run())
		w.wantStderr("ERROR:", name)
		if len(w.fake.tests()) != 0 || len(w.fake.scans()) != 0 {
			t.Fatalf("semgrep went on after %s was missing", name)
		}
	}
	w := newWorld(t, map[string]string{pyFixtureFile: ""})
	if err := os.MkdirAll(filepath.Join(w.root, pyFixtureFile), 0o750); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR:", pyFixtureFile, "not a regular file")
}

// S6
func TestARuleFileThatIsNotTheExpectedShapeIsExitTwo(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"does not decode": {"rules: [\n", "cannot decode " + ruleFile},
		"no rules":        {"other: 1\n", "has no rules"},
		"empty rules":     {"rules: []\n", "has no rules"},
		"two languages":   {"rules:\n  - id: a\n    languages: [javascript, python]\n", "rule a: languages must be exactly one"},
		"other language":  {"rules:\n  - id: a\n    languages: [go]\n", "rule a: language \"go\" is not javascript or python"},
		"no languages":    {"rules:\n  - id: a\n", "rule a: languages must be exactly one"},
		"no id":           {"rules:\n  - languages: [python]\n", "rule 1 has no id"},
		"rules not list":  {"rules: x\n", "rules must be a list"},
		"only javascript": {"rules:\n  - id: a\n    languages: [javascript]\n", "no python rules"},
		"only python":     {"rules:\n  - id: a\n    languages: [python]\n", "no javascript rules"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{ruleFile: c.text})
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ", c.want)
			if len(w.fake.tests()) != 0 || len(w.fake.scans()) != 0 {
				t.Fatal("semgrep went on after a bad rule file")
			}
		})
	}
}

// S7
func TestAFixtureWithoutAnnotationsOrMissingARuleProvesNothingAndIsExitTwo(t *testing.T) {
	cases := map[string]struct{ file, text, want string }{
		"js none":            {jsFixtureFile, "// nothing\n", jsFixtureFile + " marks no ruleid line for model-present-without-weights"},
		"js misses one rule": {jsFixtureFile, "// ruleid: model-present-without-weights\n", jsFixtureFile + " marks no ruleid line for dropdown-label-carries-explanation"},
		"py none":            {pyFixtureFile, "# ok: model-present-without-weights\n", pyFixtureFile + " marks no ruleid line for model-present-without-weights"},
		"py wrong rule":      {pyFixtureFile, "# ruleid: dropdown-label-carries-explanation\n", pyFixtureFile + " marks no ruleid line for model-present-without-weights"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{c.file: c.text})
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: " + c.want + "; the self-test proves nothing for it")
			if len(w.fake.tests()) != 0 {
				t.Fatal("the self-test ran on a fixture that proves nothing")
			}
		})
	}
}

// S7: todoruleid certifies nothing (semgrep does not fail a todoruleid line that does not match),
// so it does not count as a ruleid line.
func TestATodoruleidAnnotationDoesNotProveARule(t *testing.T) {
	w := newWorld(t, map[string]string{pyFixtureFile: "# todoruleid: model-present-without-weights\n"})
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: " + pyFixtureFile + " marks no ruleid line for model-present-without-weights")
}

// S7: two ids on one annotation line both count.
func TestOneAnnotationMayNameSeveralRules(t *testing.T) {
	w := newWorld(t, map[string]string{jsFixtureFile: "// ruleid: model-present-without-weights, dropdown-label-carries-explanation\n"})
	w.wantExit(ExitOK, w.run())
}

// S8
func TestTheSelfTestRunsTheRulesOverEachFixtureWithMetricsOff(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	got := w.fake.tests()
	rules := filepath.Join(w.root, ruleFile)
	want := [][]string{
		{"semgrep", "--test", "--metrics=off", "--disable-version-check", "--config", rules, filepath.Join(w.root, jsFixtureFile)},
		{"semgrep", "--test", "--metrics=off", "--disable-version-check", "--config", rules, filepath.Join(w.root, pyFixtureFile)},
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("self-test argv\n%v\nwant\n%v", got, want)
	}
}

// S8
func TestARuleThatFailsAFixtureIsExitOneAndNothingIsScanned(t *testing.T) {
	for _, bad := range []string{jsFixtureFile, pyFixtureFile} {
		w := newWorld(t, nil)
		w.fake.testFor = func(fixture string) proc.Result {
			if strings.HasSuffix(fixture, bad) {
				return proc.Result{Code: 1, Stdout: "1 unexpected\n", Stderr: "boom\n"}
			}
			return proc.Result{}
		}
		w.wantExit(ExitFailed, w.run())
		w.wantStderr("ERROR: "+ruleFile+" failed its fixture "+bad+":", "1 unexpected", "boom")
		if len(w.fake.scans()) != 0 {
			t.Fatal("a scan ran after the rule failed its fixture")
		}
	}
}

// S8
func TestASelfTestThatCannotRunIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.testEr = errors.New("permission denied")
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep: permission denied")
	w = newWorld(t, nil)
	w.fake.test = proc.Result{TimedOut: true}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep did not finish within")
}

// S9
func TestTheDefaultScopeIsTheJavaScriptAndPythonFilesLessTheExcludedTrees(t *testing.T) {
	w := newWorld(t, map[string]string{
		"files/home/.local/bin/stt-tool": "#!/usr/bin/env python3\nprint(1)\n",
		"files/home/.local/bin/other":    "#!/usr/bin/env bash\necho\n",
		"web/page.mjs":                   "export {};\n",
		"docs/readme.md":                 "# x\n",
		".claude/hooks-daemon/d.py":      "x\n",
		"sub/node_modules/q/i.js":        "x\n",
		"tests/fixtures/f.py":            "x = 1\n",
	})
	w.wantExit(ExitOK, w.run())
	want := []string{"extensions/panel.js", "files/home/.local/bin/stt-tool", "helpers/mod.py", "tests/fixtures/f.py", "web/page.mjs"}
	if got := w.fake.handedFiles(); !slices.Equal(got, want) {
		t.Fatalf("handed %v, want %v", got, want)
	}
	w.wantStdout("passed: 5 failed: 0\n")
}

// S9: the vendored trees are the trees table's business (discovery drops them), this gate adds
// only its own fixtures; so a table without .claude/skills judges a file there.
func TestTheVendoredTreesComeFromTheTreesTableAlone(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/qa-trees.tsv": "shell\t.semgrep\npython\t.venv\n"})
	w.wantExit(ExitOK, w.run())
	if !slices.Contains(w.fake.handedFiles(), ".claude/skills/s/run.py") || slices.Contains(w.fake.handedFiles(), ".semgrep/unrelated.py") {
		t.Fatalf("handed %v", w.fake.handedFiles())
	}
}

// S10: the shebang is parsed, not searched: a name that merely contains python is not Python.
func TestANamedFileWhoseInterpreterOnlyContainsPythonIsNotPython(t *testing.T) {
	w := newWorld(t, map[string]string{"a/t1": "#!/usr/bin/perl # python\n", "a/t2": "#!/bin/sh -c 'exec python'\n", "a/t3": "#!/usr/bin/env -S python3.12 -u\n"})
	for _, p := range []string{"a/t1", "a/t2"} {
		w.stderr.Reset()
		w.wantExit(ExitCannotRun, w.run(p))
		w.wantStderr("ERROR: " + p + " is neither JavaScript nor Python")
	}
	w.wantExit(ExitOK, w.run("a/t3"))
}

// S17: the count is of the files semgrep reports it read.
func TestPassedCountsWhatSemgrepReportsRead(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(_ string, files []string) proc.Result {
		return reportFor(t, append(slices.Clone(files), files...), nil, nil)
	}
	w.wantExit(ExitOK, w.run("helpers/mod.py"))
	w.wantStdout("passed: 1 failed: 0\n")
	w = newWorld(t, nil)
	w.fake.scan = func(_ string, files []string) proc.Result {
		return reportFor(t, append(slices.Clone(files), "extra/one.py", "extra/two.py"), nil, nil)
	}
	w.wantExit(ExitOK, w.run("helpers/mod.py"))
	w.wantStdout("passed: 3 failed: 0\n")
}

// S8: a rule file semgrep cannot load is the gate's input being unusable, exit 2 with its cause,
// not a rule that stopped matching.
func TestARuleFileSemgrepCannotLoadIsExitTwoWithItsCause(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.test = proc.Result{Code: 2, Stderr: "invalid pattern-regex\n"}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep could not run the self-test (exit 2)", "invalid pattern-regex")
	if len(w.fake.scans()) != 0 {
		t.Fatal("a scan ran")
	}
}

// S8: semgrep also exits 2 for a rule id that a fixture annotates and the rules do not hold
// (a rule edited into another name); that is a fixture the rules failed, exit 1.
func TestARuleIdMismatchIsAFailedFixtureNotAnUnusableRuleFile(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.test = proc.Result{Code: 2, Stderr: "Failing due to rule id mismatch.\n"}
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("failed its fixture", "rule id mismatch")
}

// S9 / B1: a new file git has not been told about is judged too.
func TestAnUntrackedUnignoredFileIsJudged(t *testing.T) {
	w := newWorld(t, nil)
	w.write("helpers/new.py", "x = 1\n")
	w.wantExit(ExitOK, w.run())
	if !slices.Contains(w.fake.handedFiles(), "helpers/new.py") {
		t.Fatalf("handed %v", w.fake.handedFiles())
	}
}

// S9
func TestALanguageWithNoFileLeftIsExitTwo(t *testing.T) {
	for lang, drop := range map[string]string{"JavaScript": "extensions/panel.js", "Python": "helpers/mod.py"} {
		w := newWorld(t, map[string]string{drop: "", "node_modules/p/i.js": ""})
		if lang == "JavaScript" {
			w.write(".semgrep/only.js", "x\n")
		}
		w.wantExit(ExitCannotRun, w.run())
		w.wantStderr("ERROR: ")
		if len(w.fake.scans()) != 0 {
			t.Fatalf("%s: a scan ran over nothing", lang)
		}
	}
}

// S9
func TestADiscoveryFailureIsExitTwoWithItsMessage(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/qa-trees.tsv": ""})
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: ", "qa-trees.tsv")
	if len(w.fake.scans()) != 0 {
		t.Fatal("a scan ran after discovery failed")
	}
}

// S10
func TestNamedFilesAreTheOnlyOnesJudged(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run("helpers/mod.py"))
	if got := w.fake.handedFiles(); !slices.Equal(got, []string{"helpers/mod.py"}) {
		t.Fatalf("handed %v", got)
	}
	w.wantStdout("passed: 1 failed: 0\n")
	if len(w.fake.configs) != 1 {
		t.Fatalf("%d scans, want only the Python one", len(w.fake.configs))
	}
}

// S10: the language is by name, then by a Python shebang; anything else is refused.
func TestNamedFilesAreClassifiedByNameThenByPythonShebang(t *testing.T) {
	w := newWorld(t, map[string]string{
		"a/tool":     "#!/usr/bin/env python3\n",
		"a/script.x": "#!/usr/bin/python\n",
		"a/m.mjs":    "export {};\n",
		"a/m.js":     "1;\n",
		"a/m.py":     "x\n",
	})
	w.wantExit(ExitOK, w.run("a/tool", "a/script.x", "a/m.mjs", "a/m.js", "a/m.py"))
	byLang := map[string][]string{}
	for _, c := range w.fake.configs {
		lang := "py"
		if strings.Contains(c.text, "dropdown-label") {
			lang = "js"
		}
		byLang[lang] = c.files
	}
	if !slices.Equal(byLang["js"], []string{"a/m.mjs", "a/m.js"}) || !slices.Equal(byLang["py"], []string{"a/tool", "a/script.x", "a/m.py"}) {
		t.Fatalf("by language %v", byLang)
	}
}

// S10 / B4
func TestANamedFileThatIsNeitherLanguageIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"a/run.bash": "#!/usr/bin/env bash\n", "a/notes.txt": "hi\n", "a/empty": ""})
	w.write("a/empty", "")
	for _, p := range []string{"a/run.bash", "a/notes.txt", "a/empty"} {
		w.stderr.Reset()
		w.wantExit(ExitCannotRun, w.run(p))
		w.wantStderr("ERROR: " + p + " is neither JavaScript nor Python")
	}
	if len(w.fake.scans()) != 0 {
		t.Fatal("a scan ran over a file that is neither language")
	}
}

// S10
func TestANamedFileInAnExcludedTreeIsJudgedBecauseItWasNamed(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run(".semgrep/unrelated.py"))
	if got := w.fake.handedFiles(); !slices.Equal(got, []string{".semgrep/unrelated.py"}) {
		t.Fatalf("handed %v", got)
	}
}

// S10
func TestNamedPathsAreMadeRelativeAndJudgedOnce(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run("./helpers/mod.py", filepath.Join(w.root, "helpers", "mod.py"), "helpers//mod.py"))
	if got := w.fake.handedFiles(); !slices.Equal(got, []string{"helpers/mod.py"}) {
		t.Fatalf("handed %v", got)
	}
}

// S10
func TestAnAbsolutePathThroughASymlinkedCheckoutIsAccepted(t *testing.T) {
	w := newWorld(t, nil)
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitOK, w.run(filepath.Join(link, "helpers", "mod.py")))
	if got := w.fake.handedFiles(); !slices.Equal(got, []string{"helpers/mod.py"}) {
		t.Fatalf("handed %v", got)
	}
}

// S10
func TestPathsOutsideTheRootAreExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	other := filepath.Join(t.TempDir(), "x.py")
	if err := os.WriteFile(other, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{other, "../x.py", "helpers/../../x.py"} {
		w.stderr.Reset()
		w.wantExit(ExitCannotRun, w.run(p))
		w.wantStderr("ERROR: ", "outside the repository root")
	}
}

// S10
func TestAnUnreadableNamedFileIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, w.run("helpers/gone.py"))
	w.wantStderr("ERROR: ", "helpers/gone.py")
	w = newWorld(t, nil)
	w.wantExit(ExitCannotRun, w.run("helpers"))
	w.wantStderr("ERROR: ", "helpers", "not a regular file")
	if len(w.fake.scans()) != 0 {
		t.Fatal("a scan ran")
	}
}

// S11
func TestEachLanguageIsScannedWithOnlyItsOwnRulesInAPrivateFileThatIsRemoved(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	if len(w.fake.configs) != 2 {
		t.Fatalf("%d scans, want one per language", len(w.fake.configs))
	}
	for _, c := range w.fake.configs {
		isJS := strings.Contains(c.text, "dropdown-label-carries-explanation")
		if isJS {
			if strings.Count(c.text, "id: ") != 2 || strings.Contains(c.text, "python") {
				t.Fatalf("the JavaScript rule file:\n%s", c.text)
			}
		} else if strings.Count(c.text, "id: ") != 1 || !strings.Contains(c.text, "python") || strings.Contains(c.text, "javascript") {
			t.Fatalf("the Python rule file:\n%s", c.text)
		}
		if !strings.Contains(c.text, "(?s)^.(?:.*(?:—|\\(|\\)).*|.{25,}).$") && isJS {
			t.Fatalf("a rule's pattern did not survive re-encoding:\n%s", c.text)
		}
		if c.mode != 0o600 {
			t.Fatalf("rule file mode %v", c.mode)
		}
		if _, err := os.Stat(c.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the rule file %s outlived the run: %v", c.path, err)
		}
		if strings.HasPrefix(c.path, w.root) {
			t.Fatalf("the rule file was written into the checkout: %s", c.path)
		}
	}
}

// S11: the temporary rule files are removed when the run fails too.
func TestTheRuleFilesAreRemovedWhenTheScanFails(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(string, []string) proc.Result { return proc.Result{Code: 2, Stderr: "bad"} }
	w.wantExit(ExitCannotRun, w.run())
	if len(w.fake.configs) == 0 {
		t.Fatal("no scan ran")
	}
	for _, c := range w.fake.configs {
		if _, err := os.Stat(c.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the rule file %s outlived the run: %v", c.path, err)
		}
	}
}

// S11
func TestALanguageWithNoFileToJudgeIsNotScanned(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run("extensions/panel.js"))
	if len(w.fake.configs) != 1 || !strings.Contains(w.fake.configs[0].text, "dropdown-label") {
		t.Fatalf("scans %v", w.fake.configs)
	}
}

// S12
func TestAScanHandsTheFilesByNameFromTheRoot(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run("helpers/mod.py"))
	argv := w.fake.configs[0].argv
	want := []string{
		"semgrep", "--metrics=off", "--disable-version-check", "--config", w.fake.configs[0].path,
		"--no-rewrite-rule-ids", "--scan-unknown-extensions", "--json", "--quiet", "--max-target-bytes=0", "helpers/mod.py",
	}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv\n%v\nwant\n%v", argv, want)
	}
	if w.fake.dirs[len(w.fake.dirs)-1] != w.root {
		t.Fatalf("ran in %q", w.fake.dirs)
	}
}

// S13
func TestAScanThatDidNotGiveAReportIsExitTwo(t *testing.T) {
	cases := map[string]proc.Result{
		"exit 2":       {Code: 2, Stderr: "rules broke\n", Stdout: `{"paths":{"scanned":[]}}`},
		"exit 7":       {Code: 7, Stderr: "rules broke\n"},
		"not json":     {Stdout: "garbage"},
		"empty":        {},
		"no paths":     {Stdout: `{"results":[],"errors":[]}`},
		"timed out":    {TimedOut: true},
		"left a child": {Leftover: true},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.fake.scan = func(string, []string) proc.Result { return res }
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ")
			if w.stdout.Len() != 0 {
				t.Fatalf("stdout %q", w.stdout.String())
			}
		})
	}
	w := newWorld(t, nil)
	w.fake.scan = func(string, []string) proc.Result { return proc.Result{Code: 2, Stderr: "rules broke\n"} }
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("rules broke")
}

// S13: exit 1 is a report, not a failure to run.
func TestSemgrepExitOneWithAReportIsAReport(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(_ string, files []string) proc.Result {
		res := reportFor(t, files, nil, nil)
		res.Code = 1
		return res
	}
	w.wantExit(ExitOK, w.run())
}

// S14
func TestAParseErrorInTheReportIsExitTwoListingTheFile(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(_ string, files []string) proc.Result {
		return reportFor(t, files, nil, []map[string]string{{"path": "helpers/mod.py", "message": "Syntax error"}})
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: ", "helpers/mod.py", "Syntax error")
}

// S15
func TestALostFileIsNamedAndNotAPass(t *testing.T) {
	w := newWorld(t, map[string]string{"helpers/mod2.py": "x\n"})
	w.fake.scan = func(_ string, files []string) proc.Result {
		return reportFor(t, slices.DeleteFunc(slices.Clone(files), func(f string) bool { return f == "helpers/mod2.py" }), nil, nil)
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep did not scan Python files it was handed:", "    helpers/mod2.py", "is not a pass")
	if w.stdout.Len() != 0 {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}

// S15: a "./" prefix on a scanned path is the same file.
func TestScannedPathsWithADotSlashAreTheSameFiles(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(_ string, files []string) proc.Result {
		prefixed := make([]string, len(files))
		for i, f := range files {
			prefixed[i] = "./" + f
		}
		return reportFor(t, prefixed, nil, nil)
	}
	w.wantExit(ExitOK, w.run())
}

// S15
func TestScanningNoFileAtAllIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(string, []string) proc.Result { return reportFor(t, nil, nil, nil) }
	w.wantExit(ExitCannotRun, w.run("helpers/mod.py"))
	w.wantStderr("ERROR: ")
}

// S16
func TestFindingsAreSortedByPathThenNumericLineWithoutDuplicates(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(lang string, files []string) proc.Result {
		if lang == "js" {
			return reportFor(t, files, []jsonResult{hit("extensions/panel.js", 3)}, nil)
		}
		return reportFor(t, files, []jsonResult{hit("helpers/mod.py", 10), hit("helpers/mod.py", 9), hit("helpers/mod.py", 9)}, nil)
	}
	w.wantExit(ExitFailed, w.run())
	want := "extensions/panel.js:3: model-present-without-weights\n" +
		"helpers/mod.py:9: model-present-without-weights\n" +
		"helpers/mod.py:10: model-present-without-weights\n" +
		"speech-to-text rules: 3 finding(s) in 2 file(s). See CLAUDE/QA.md under each identifier.\n" +
		"passed: 0 failed: 2\n"
	if w.stdout.String() != want {
		t.Fatalf("stdout\n%s\nwant\n%s", w.stdout.String(), want)
	}
}

// S16: the counts are of files, so one file with many findings is one failed file.
func TestPassedCountsFilesNotFindings(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(lang string, files []string) proc.Result {
		if lang == "js" {
			return reportFor(t, files, nil, nil)
		}
		return reportFor(t, files, []jsonResult{hit("helpers/mod.py", 1), hit("helpers/mod.py", 2)}, nil)
	}
	w.wantExit(ExitFailed, w.run())
	w.wantStdout("2 finding(s) in 1 file(s)", "passed: 1 failed: 1\n")
}

// S3: semgrep's own words name files by their absolute path; the checkout path is taken out of
// them before they are shown.
func TestSemgrepOutputNamingTheCheckoutIsScrubbed(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.testFor = func(fixture string) proc.Result {
		return proc.Result{Code: 1, Stdout: "mismatch file=" + fixture + "\n", Stderr: "in " + w.root + "\n"}
	}
	w.wantExit(ExitFailed, w.run())
	if strings.Contains(w.stderr.String(), w.root) {
		t.Fatalf("the checkout path leaked:\n%s", w.stderr.String())
	}
	w.wantStderr("mismatch file=.semgrep/speech-to-text.js")
	w = newWorld(t, nil)
	w.fake.scan = func(string, []string) proc.Result {
		return proc.Result{Code: 2, Stderr: "bad file " + w.root + "/helpers/mod.py\n"}
	}
	w.wantExit(ExitCannotRun, w.run())
	if strings.Contains(w.stderr.String(), w.root) {
		t.Fatalf("the checkout path leaked:\n%s", w.stderr.String())
	}
	w.wantStderr("bad file helpers/mod.py")
}

// S17
func TestACleanRunPrintsThePassedLineOnly(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	if w.stdout.String() != "passed: 2 failed: 0\n" {
		t.Fatalf("stdout %q", w.stdout.String())
	}
}
