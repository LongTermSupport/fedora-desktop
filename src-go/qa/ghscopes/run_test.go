package ghscopes

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// G1
func TestHelpIsUsageOnStdoutExitZero(t *testing.T) {
	got := newWorld(t, nil).main("--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-gh-scopes")
}

// G1
func TestAnUnknownArgumentOrMissingValueIsExitTwoWithUsage(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"--root"}, {"--scripts"}} {
		got := newWorld(t, nil).main(args...)
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", "usage: qa-gh-scopes")
	}
}

// G1: an empty or flag-like --root/--scripts value is refused, not read as "not given".
func TestAnEmptyOrFlagLikeDirectoryValueIsRefused(t *testing.T) {
	for _, args := range [][]string{{"--root", ""}, {"--scripts", ""}, {"--root", "--scripts"}} {
		got := newWorld(t, nil).main(args...)
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR: "+args[0]+" needs a directory", "usage: qa-gh-scopes")
	}
}

// G1
func TestScriptsDirNamesTheRootAsItsParent(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main("--scripts", filepath.Join(w.root, "scripts")), ExitOK)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// G2
func TestAStdoutThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stderr strings.Builder
	code := Main(nil, Env{Stdout: failWriter{}, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	if code != ExitCannotRun || !strings.Contains(stderr.String(), "ERROR: cannot write to stdout") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
}

// G4
func TestSemgrepThatCannotRunIsExitTwoAndNothingElseRuns(t *testing.T) {
	for _, set := range []func(*fakeRun){
		func(f *fakeRun) { f.noSemgrep = true },
		func(f *fakeRun) { f.versionCode = 1; f.versionErr = "bad python env" },
	} {
		w := newWorld(t, nil)
		set(w.run)
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR: semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)")
		if w.run.called("git") {
			t.Fatal("git ran before semgrep was found")
		}
	}
}

// G4
func TestTheRealCauseOfASemgrepThatCannotRunIsShown(t *testing.T) {
	w := newWorld(t, nil)
	w.run.noSemgrep = true
	wantIn(t, "stderr", w.main().stderr, "executable file not found in $PATH")
	w = newWorld(t, nil)
	w.run.versionCode = 1
	w.run.versionErr = "bad python env"
	wantIn(t, "stderr", w.main().stderr, "semgrep --version exited 1", "bad python env")
}

// G5
func TestAMissingRuleOrFixtureIsExitTwoNamingIt(t *testing.T) {
	for _, name := range []string{ruleFile, fixtureFile} {
		w := newWorld(t, map[string]string{name: ""})
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", name)
	}
}

// G6
func TestARuleThatFailsItsFixtureIsExitOneAndNothingIsScanned(t *testing.T) {
	w := newWorld(t, nil)
	w.run.testCode = 1
	w.run.testOut = "1 unexpected\n"
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stderr", got.stderr, "ERROR: gh-scope-outside-ssot failed its fixture (.semgrep/gh-scopes.fixture):", "1 unexpected")
	if w.run.scanArgv != nil || w.run.called("git") {
		t.Fatal("a scan ran after the rule failed its fixture")
	}
}

// G6
func TestTheSelfTestRunsTheRuleOverTheFixtureWithMetricsOff(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main(), ExitOK)
	var test []string
	for _, c := range w.run.calls {
		if slices.Contains(c, "--test") {
			test = c
		}
	}
	want := []string{"semgrep", "--test", "--config", filepath.Join(w.root, ruleFile), filepath.Join(w.root, fixtureFile), "--metrics=off"}
	if !slices.Equal(test, want) {
		t.Fatalf("self-test argv %v, want %v", test, want)
	}
}

// G6
func TestAFixtureThatMarksNoRuleidLineProvesNothingAndIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{fixtureFile: "# fixture\n# ok: gh-scope-outside-ssot\n"})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: "+fixtureFile+" marks no ruleid line; the self-test proves nothing")
	if w.run.scanArgv != nil || w.run.called("git") {
		t.Fatal("a scan ran over a fixture that proves nothing")
	}
}

// G6: a todoruleid line certifies nothing.
func TestATodoruleidLineProvesNothingAndIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{fixtureFile: "# todoruleid: gh-scope-outside-ssot\n"})
	wantExit(t, w.main(), ExitCannotRun)
}

// G6, G10
func TestAProcessThatTimesOutIsExitTwo(t *testing.T) {
	for _, which := range []string{"test", "scan"} {
		w := newWorld(t, nil)
		w.run.timeout = which
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "did not finish")
	}
}

// G7
func TestGitFailingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.gitCode = 128
	w.run.gitErr = "fatal: not a git repository"
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: cannot list the files git would publish under .", "not a git repository")
}

// G7
func TestTheFileListIsWhatGitWouldPublish(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main(), ExitOK)
	if !w.run.called("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard") {
		t.Fatalf("calls %v", w.run.calls)
	}
}

// G7
func TestSymlinksAndFilesGoneFromDiskAreNotHandedOver(t *testing.T) {
	w := newWorld(t, nil)
	w.link("alias.txt", "a.txt")
	w.run.listed = append(w.run.listed, "deleted.txt")
	wantExit(t, w.main(), ExitOK)
	files := handed(w.run.scanArgv)
	if slices.Contains(files, "alias.txt") || slices.Contains(files, "deleted.txt") || !slices.Contains(files, "a.txt") {
		t.Fatalf("handed over %v", files)
	}
}

// G7
func TestAFileThatCannotBeLookedAtIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, nil)
	w.run.listed = append(w.run.listed, "a.txt/inside")
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR:", "a.txt/inside")
}

// G8
func TestNoFileToScanIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.listed = nil
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: git named no files to scan; nothing was checked")
}

// G9
func TestFilesAreHandedByNameFromTheRootNotAsADirectory(t *testing.T) {
	w := newWorld(t, map[string]string{"tests/x.bash": "x\n"})
	wantExit(t, w.main(), ExitOK)
	argv := w.run.scanArgv
	i := slices.Index(argv, "--max-target-bytes=0")
	want := []string{"semgrep", "--config", filepath.Join(w.root, ruleFile), "--json", "--quiet", "--metrics=off", "--max-target-bytes=0"}
	if i < 0 || !slices.Equal(argv[:i+1], want) {
		t.Fatalf("argv %v, want head %v", argv, want)
	}
	files := handed(argv)
	if !slices.Contains(files, "tests/x.bash") || slices.Contains(files, ".") {
		t.Fatalf("files %v", files)
	}
	if got := w.run.dirs[len(w.run.dirs)-1]; got != w.root {
		t.Fatalf("scan ran in %q, want the root", got)
	}
}

// G10
func TestASemgrepFailureOrNoReportIsExitTwoWithItsStderr(t *testing.T) {
	cases := map[string]func(*fakeRun){
		"exit 2":   func(f *fakeRun) { f.scanCode = 2; f.scanStderr = "boom" },
		"no JSON":  func(f *fakeRun) { f.scan = func([]string) string { return "" }; f.scanStderr = "boom" },
		"not JSON": func(f *fakeRun) { f.scan = func([]string) string { return "oops" }; f.scanStderr = "boom" },
		"no paths": func(f *fakeRun) {
			f.scan = func([]string) string { return `{"results":[],"errors":[]}` }
			f.scanStderr = "boom"
		},
	}
	for name, set := range cases {
		w := newWorld(t, nil)
		set(w.run)
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, name+" stderr", got.stderr, "ERROR: semgrep failed", "boom")
	}
}

// G10
func TestSemgrepExitOneWithAReportIsAReport(t *testing.T) {
	w := newWorld(t, nil)
	w.run.scanCode = 1
	wantExit(t, w.main(), ExitOK)
}

// G11
func TestAnErrorInTheReportIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.scan = func(files []string) string {
		return report(scannable(files), nil, nil, `{"type":"Timeout","path":"a.txt"}`)
	}
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR:", "a.txt", "Timeout")
}

// G12
func TestAHandedFileThatWasNotScannedIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n", "c.txt": "x\n"})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "did not scan 2 file(s)", "    b.txt", "    c.txt", "not a pass")
}

// G12
func TestFilesTheRuleExcludesAreNotExpectedToBeScanned(t *testing.T) {
	w := newWorld(t, map[string]string{
		"vars/github-required-scopes.yml": "x\n",
		"helpers/github_scopes/a/b.py":    "x\n",
		"CLAUDE/Plan/00001-x/PLAN.md":     "x\n",
		"CLAUDE/Plan/PLAN.md":             "x\n",
		"untracked/deep/note.txt":         "x\n",
	})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	wantExit(t, w.main(), ExitOK)
}

// G12
func TestAGlobMatchesAtAnyDepthLikeAGitignorePattern(t *testing.T) {
	w := newWorld(t, map[string]string{
		"sub/vars/github-required-scopes.yml": "x\n",
		"x/CLAUDE/Plan/deep.md":               "x\n",
		"y/helpers/github_scopes/a.py":        "x\n",
	})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	wantExit(t, w.main(), ExitOK)
}

// G12
func TestALeadingSlashAnchorsAGlobToTheRoot(t *testing.T) {
	rule := "rules:\n  - id: a\n    paths:\n      exclude: ['/top.txt', '.semgrep/*']\n"
	w := newWorld(t, map[string]string{ruleFile: rule, "top.txt": "x\n", "sub/top.txt": "x\n"})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "did not scan 1 file(s)", "    sub/top.txt")
}

// G12
func TestAnExcludeThatIsNotAStringIsExitTwo(t *testing.T) {
	for _, list := range []string{"[123, true]", "[[a]]", "[{a: b}]", "[null]"} {
		w := newWorld(t, map[string]string{ruleFile: "rules:\n  - id: x\n    paths:\n      exclude: " + list + "\n"})
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", ruleFile)
	}
}

// G12
func TestAGlobStarStaysWithinOneSegment(t *testing.T) {
	w := newWorld(t, map[string]string{"helpers/github_scopes.txt": "x\n", "CLAUDE/Plan/00001-x/notes.txt": "x\n"})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "    CLAUDE/Plan/00001-x/notes.txt", "    helpers/github_scopes.txt")
}

// G12
func TestARuleWithoutPathExcludesIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{ruleFile: "rules:\n  - id: x\n    languages: [generic]\n"})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR:", "paths.exclude")
}

// G12
func TestTheRuleIsDecodedAsYamlNotReadByLine(t *testing.T) {
	rule := "rules:\n  - id: a\n    paths: {exclude: ['one.txt', \"two/*.md\", '.semgrep/*']}\n  - id: b\n    paths:\n      exclude:\n      - three.txt\n"
	w := newWorld(t, map[string]string{ruleFile: rule, "one.txt": "x\n", "two/n.md": "x\n", "three.txt": "x\n"})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, nil) }
	wantExit(t, w.main(), ExitOK)
}

// G12
func TestARuleThatIsNotValidYamlOrHasTheWrongShapeIsExitTwo(t *testing.T) {
	for _, rule := range []string{"rules: [unclosed\n", "rules:\n  - paths:\n      exclude: not-a-list\n", "rules: 3\n", ""} {
		w := newWorld(t, map[string]string{ruleFile: rule})
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", ruleFile)
	}
}

// G12
func TestAnExcludeThatIsNotAListItemIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{ruleFile: "rules:\n  - id: x\n    paths:\n      exclude:\n        a: b\n"})
	wantExit(t, w.main(), ExitCannotRun)
}

// G13
func TestSkippedFilesAreAccountedForAndPrinted(t *testing.T) {
	w := newWorld(t, map[string]string{"big.bin": "x\n"})
	w.run.scan = func([]string) string { return report([]string{"a.txt"}, nil, []string{"big.bin"}) }
	got := w.main()
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "⚠ gh-scopes: 1 file(s) skipped by semgrep", "    big.bin", "passed: 1 failed: 0")
}

// G14
func TestAScanThatReadNothingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.run.scan = func([]string) string { return report(nil, nil, []string{"a.txt"}) }
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR: semgrep scanned no file")
}

// G15
func TestFindingsAreSortedDistinctAndCounted(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n", "c.txt": "x\n"})
	w.run.scan = func(files []string) string {
		return report(scannable(files), []hit{{"./c.txt", 9}, {"b.txt", 3}, {"b.txt", 3}, {"b.txt", 1}}, nil)
	}
	got := w.main()
	wantExit(t, got, ExitFailed)
	want := "b.txt:1  gh-scope-outside-ssot\n" +
		"b.txt:3  gh-scope-outside-ssot\n" +
		"c.txt:9  gh-scope-outside-ssot\n" +
		"gh-scope-outside-ssot: 3 line(s) in 2 file(s) name a GitHub scope outside vars/github-required-scopes.yml. See CLAUDE/QA.md \"gh-scope-outside-ssot\".\n" +
		"passed: 1 failed: 2\n"
	if got.stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got.stdout, want)
	}
}

// G16
func TestACleanRunPrintsThePassedLineOfTheScannedCount(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n"})
	got := w.main()
	wantExit(t, got, ExitOK)
	if got.stdout != "passed: 2 failed: 0\n" || got.stderr != "" {
		t.Fatalf("stdout %q stderr %q", got.stdout, got.stderr)
	}
}

// G3
func TestARootReachedThroughASymlinkNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, nil)
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.run.scan = func(files []string) string { return report(scannable(files), []hit{{"a.txt", 2}}, nil) }
	got := w.main("--root", link)
	wantExit(t, got, ExitFailed)
	for _, leak := range []string{w.root, link} {
		if strings.Contains(got.stdout, leak) || strings.Contains(got.stderr, leak) {
			t.Fatalf("a checkout path leaked:\n%s\n%s", got.stdout, got.stderr)
		}
	}
}

// G3
func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "cannot resolve the repository root")
}
