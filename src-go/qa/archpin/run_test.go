package archpin

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHelpIsUsageOnStdoutExitZero(t *testing.T) {
	got := newWorld(t, nil).main("--help")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "usage: qa-arch-pin")
}

func TestAnUnknownArgumentIsExitTwoWithUsage(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"--root"}, {"--root", ""}} {
		got := newWorld(t, nil).main(args...)
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", "usage: qa-arch-pin")
	}
}

func TestScriptsDirNamesTheRootAsItsParent(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--scripts", filepath.Join(w.root, "scripts"))
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "passed: 1 failed: 0")
}

func TestAnUnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main("--root", filepath.Join(w.root, "missing")), ExitCannotRun)
}

// A root reached through a symlink is resolved, so the paths semgrep prints still match.
func TestASymlinkedRootIsResolved(t *testing.T) {
	w := newWorld(t, nil)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	wantExit(t, w.main("--root", link), ExitOK)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestAStdoutThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	var stderr strings.Builder
	code := Main(nil, Env{Stdout: failWriter{}, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	if code != ExitCannotRun || !strings.Contains(stderr.String(), "ERROR: cannot write to stdout") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	if code := Main([]string{"--help"}, Env{Stdout: failWriter{}, Stderr: &stderr, Cwd: w.root, Runner: w.run}); code != ExitCannotRun {
		t.Fatalf("--help to a closed stdout: exit %d", code)
	}
}

func TestAToolThatCannotRunIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"no semgrep":       func(f *fakeRun) { f.noSemgrep = true },
		"semgrep broken":   func(f *fakeRun) { f.versionCode = 1 },
		"git fails":        func(f *fakeRun) { f.gitCode = 128 },
		"scan timed out":   func(f *fakeRun) { f.timeout = true },
		"scan crashed":     func(f *fakeRun) { f.scanCode = 2 },
		"scan not json":    func(f *fakeRun) { f.garbage = true },
		"a parse error":    func(f *fakeRun) { f.errs = []string{`{"path":"a.txt","message":"boom"}`} },
		"a file not read":  func(f *fakeRun) { f.drop = []string{"a.txt"} },
		"git lists none":   func(f *fakeRun) { f.listed = []string{"gone.txt"} },
		"git lists a link": func(f *fakeRun) { f.listed = nil },
	} {
		w := newWorld(t, nil)
		set(w.run)
		got := w.main()
		if got.code != ExitCannotRun || !strings.Contains(got.stderr, "ERROR:") {
			t.Errorf("%s: exit %d\nstdout: %s\nstderr: %s", name, got.code, got.stdout, got.stderr)
		}
	}
}

func TestTheRuleAndItsDataMustBeRegularFiles(t *testing.T) {
	for _, rel := range []string{ruleFile, fixtureFile, allowlistFile} {
		got := newWorld(t, map[string]string{rel: ""}).main()
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, rel)
	}
	w := newWorld(t, map[string]string{ruleFile: ""})
	if err := os.MkdirAll(filepath.Join(w.root, ruleFile), 0o750); err != nil {
		t.Fatal(err)
	}
	wantIn(t, "stderr", w.main().stderr, "not a regular file")
}

func TestARuleWithoutExcludesOrAFixtureWithoutRuleIDIsExitTwo(t *testing.T) {
	got := newWorld(t, map[string]string{ruleFile: "rules:\n  - id: arch-pinned-artefact\n"}).main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "paths.exclude")
	got = newWorld(t, map[string]string{fixtureFile: "# nothing marked\n"}).main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "marks no ruleid line")
}

func TestAMalformedAllowlistIsExitTwo(t *testing.T) {
	got := newWorld(t, map[string]string{allowlistFile: "a.txt\tmany\tnote\n"}).main()
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, allowlistFile+":1")
}

func TestARuleThatFailsItsFixtureIsExitOne(t *testing.T) {
	w := newWorld(t, nil)
	w.run.testCode = 1
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stderr", got.stderr, "arch-pinned-artefact failed its fixture")
}

func TestExcludedFilesAreNotHandedToSemgrep(t *testing.T) {
	w := newWorld(t, nil)
	wantExit(t, w.main(), ExitOK)
	if !slices.Equal(w.run.handed, []string{"a.txt"}) {
		t.Fatalf("handed %v", w.run.handed)
	}
}

func TestANewInstanceFailsAndIsNamedByLine(t *testing.T) {
	w := newWorld(t, map[string]string{"b/Dockerfile": "x\ny\n"})
	w.run.hits = []hit{{"b/Dockerfile", 2}, {"b/Dockerfile", 2}}
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "b/Dockerfile:2  arch-pinned-artefact", "not on "+allowlistFile, `CLAUDE/QA.md "arch-pinned-artefact"`, "passed: 1 failed: 1")
	if strings.Count(got.stdout, "b/Dockerfile:2 ") != 1 {
		t.Fatalf("a repeated match is one finding:\n%s", got.stdout)
	}
}

func TestKnownInstancesNamedByTheirTextPassAndAreCounted(t *testing.T) {
	w := newWorld(t, map[string]string{
		"b/Dockerfile":  "RUN  get\tx86_64\ny amd64\n",
		allowlistFile:   "# ratchet\n" + v2 + "b/Dockerfile\tRUN get x86_64\tyq: arm64 build exists\nb/Dockerfile\ty amd64\tnote\n",
		"c/clean.bash":  "echo\n",
		"docs/other.md": "x86_64\n",
	})
	w.run.hits = []hit{{"./b/Dockerfile", 1}, {"b/Dockerfile", 2}}
	got := w.main()
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "2 known instance(s) in 1 file(s)", "passed: 3 failed: 0")
}

// One pin fixed and another added beside it keeps the count; the gate still fails.
func TestAFixedPinDoesNotPayForANewOne(t *testing.T) {
	w := newWorld(t, map[string]string{
		"b/Dockerfile": "new amd64\n",
		allowlistFile:  v2 + "b/Dockerfile\told x86_64\tnote\n",
	})
	w.run.hits = []hit{{"b/Dockerfile", 1}}
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "b/Dockerfile:1  arch-pinned-artefact", "1 finding(s) that no row of", "line 2 names a finding the file no longer has", "passed: 1 failed: 1")
}

func TestARowAddedSinceTheMergeBaseFails(t *testing.T) {
	list := v2 + "b/Dockerfile\tx amd64\tnote\n"
	w := newWorld(t, map[string]string{"b/Dockerfile": "x amd64\n", allowlistFile: list})
	w.run.atRef["base0"] = v2
	w.run.hits = []hit{{"b/Dockerfile", 1}}
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "line 2 is a row base0 does not have", "passed: 1 failed: 1")
	if !slices.Equal(w.run.gitCalls, []string{"ls-files", "symbolic-ref", "merge-base", "rev-parse", "ls-tree", "show"}) {
		t.Fatalf("git calls %v", w.run.gitCalls)
	}
}

// QA_BASH_LIMITS_BASE, the base CI sets for every ratchet, is used as it is: no merge-base.
func TestTheBaseVariableNamesTheBase(t *testing.T) {
	w := newWorld(t, nil)
	w.env = map[string]string{"QA_BASH_LIMITS_BASE": "prev0"}
	w.run.originHead = ""
	w.run.atRef = map[string]string{"prev0": "b\t1\tnote\n"}
	wantExit(t, w.main(), ExitOK)
	if slices.Contains(w.run.gitCalls, "merge-base") {
		t.Fatalf("git calls %v", w.run.gitCalls)
	}
}

// A base from before the gate landed has no allowlist: the list is held to the first copy
// committed on HEAD's history, the baseline accepted with the rule.
func TestABaseWithoutTheAllowlistComparesWithItsFirstCommittedCopy(t *testing.T) {
	w := newWorld(t, map[string]string{"b/Dockerfile": "x amd64\ny amd64\n", allowlistFile: v2 + "b/Dockerfile\tx amd64\tn\nb/Dockerfile\ty amd64\tn\n"})
	w.run.atRef = map[string]string{"first0": "b/Dockerfile\t1\tnote\n"}
	w.run.added = []string{"later0", "first0"}
	w.run.hits = []hit{{"b/Dockerfile", 1}, {"b/Dockerfile", 2}}
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "2 row(s) on "+allowlistFile+", above the 1 at first0")
}

func TestNoBaseToCompareWithIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*world){
		"no origin HEAD":       func(w *world) { w.run.originHead = "" },
		"a base that is not":   func(w *world) { w.env = map[string]string{"QA_BASH_LIMITS_BASE": "nope"} },
		"never committed":      func(w *world) { w.run.atRef = map[string]string{} },
		"a broken base list":   func(w *world) { w.run.atRef["base0"] = "a\tx\n" },
		"a format 1 list here": func(w *world) { w.write(allowlistFile, "a.txt\t1\tnote\n") },
	} {
		w := newWorld(t, nil)
		set(w)
		got := w.main()
		if got.code != ExitCannotRun || !strings.Contains(got.stderr, "ERROR:") {
			t.Errorf("%s: exit %d\nstdout: %s\nstderr: %s", name, got.code, got.stdout, got.stderr)
		}
	}
}
