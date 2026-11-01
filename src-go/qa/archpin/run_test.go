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

func TestKnownInstancesAtTheirRecordPassAndAreCounted(t *testing.T) {
	w := newWorld(t, map[string]string{
		"b/Dockerfile":  "x\ny\n",
		allowlistFile:   "# ratchet\nb/Dockerfile\t2\tyq: arm64 build exists\n",
		"c/clean.bash":  "echo\n",
		"docs/other.md": "x86_64\n",
	})
	w.run.hits = []hit{{"./b/Dockerfile", 1}, {"b/Dockerfile", 2}}
	got := w.main()
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "2 known instance(s) in 1 file(s)", "passed: 3 failed: 0")
}

func TestARatchetBreachFailsTheGate(t *testing.T) {
	w := newWorld(t, map[string]string{
		"b/Dockerfile": "x\ny\n",
		allowlistFile:  "b/Dockerfile\t2\tnote\n",
	})
	w.run.hits = []hit{{"b/Dockerfile", 1}}
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "lower its record from 2 to 1", "passed: 1 failed: 1")
}
