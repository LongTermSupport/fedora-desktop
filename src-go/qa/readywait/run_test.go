package readywait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

const goodFixture = "#!/usr/bin/env bash\nw &\np=$!\n# ruleid: ready-wait-ignores-child-exit\nuntil [ -e r ]; do\n\tsleep 1\ndone\n"

const waitingScript = "#!/usr/bin/env bash\nw &\np=$!\nuntil [ -e r ]; do\n\tsleep 1\ndone\n"

// fakeRunner answers semgrep from its fields and runs the real git, which only lists files.
type fakeRunner struct {
	version   proc.Result
	versionEr error
	test      proc.Result
	scan      func(argv []string) proc.Result
	argvs     [][]string
}

func (f *fakeRunner) Run(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	if cmd.Argv[0] == "git" {
		return proc.OS{}.Run(ctx, cmd)
	}
	f.argvs = append(f.argvs, append([]string{cmd.Dir}, cmd.Argv...))
	switch {
	case slices.Contains(cmd.Argv, "--version"):
		return f.version, f.versionEr
	case slices.Contains(cmd.Argv, "--test"):
		return f.test, nil
	}
	return f.scan(cmd.Argv), nil
}

// semgrepScans is the argv of each scan run (not the version or the self-test).
func (f *fakeRunner) semgrepScans() [][]string {
	var scans [][]string
	for _, argv := range f.argvs {
		if !slices.Contains(argv, "--version") && !slices.Contains(argv, "--test") {
			scans = append(scans, argv)
		}
	}
	return scans
}

type jsonResult = semgrepreport.Result

// reportFor is semgrep's JSON naming the files handed over as scanned, with these results.
func reportFor(t *testing.T, scanned []string, results []jsonResult, errs []map[string]string) proc.Result {
	t.Helper()
	data, err := json.Marshal(map[string]any{"results": results, "errors": errs, "paths": map[string]any{"scanned": scanned}})
	if err != nil {
		t.Fatal(err)
	}
	return proc.Result{Stdout: string(data)}
}

// handed is the files after the last option of a semgrep scan argv.
func handed(argv []string) []string {
	return argv[slices.Index(argv, "--quiet")+1:]
}

type world struct {
	t      *testing.T
	root   string
	fake   *fakeRunner
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root}
	w.fake = &fakeRunner{version: proc.Result{Stdout: "1.0\n"}}
	w.fake.scan = func(argv []string) proc.Result { return reportFor(t, handed(argv), nil, nil) }
	all := map[string]string{
		".semgrep/ready-wait.yml":     "rules: []\n",
		".semgrep/ready-wait.py":      "# fixture\n",
		".semgrep/ready-wait.bash":    goodFixture,
		"scripts/qa-trees.tsv":        "shell\t.semgrep\npython\t.venv\n",
		"scripts/tool.bash":           "#!/usr/bin/env bash\necho hi\n",
		"helpers/mod.py":              "print('hi')\n",
		".semgrep/ignored-fixture.py": "print('x')\n",
	}
	for k, v := range files {
		if v == "" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	for rel, text := range all {
		w.write(rel, text)
	}
	w.track()
	return w
}

// track makes git track every file written so far.
func (w *world) track() {
	w.t.Helper()
	for _, argv := range [][]string{{"git", "init", "-q"}, {"git", "add", "-A"}} {
		if res, err := (proc.OS{}).Run(context.Background(), proc.Cmd{Argv: argv, Dir: w.root}); err != nil || res.Code != 0 {
			w.t.Fatalf("%v: %v %+v", argv, err, res)
		}
	}
}

func (w *world) write(rel, text string) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) env() Env {
	return Env{Stdout: &w.stdout, Stderr: &w.stderr, Cwd: w.root, Runner: w.fake}
}

func (w *world) run(args ...string) ExitCode {
	w.t.Helper()
	return Main(append([]string{"--root", w.root}, args...), w.env())
}

func (w *world) wantExit(code ExitCode, got ExitCode) {
	w.t.Helper()
	if got != code {
		w.t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, code, w.stdout.String(), w.stderr.String())
	}
}

func (w *world) wantStderr(parts ...string) {
	w.t.Helper()
	for _, p := range parts {
		if !strings.Contains(w.stderr.String(), p) {
			w.t.Fatalf("stderr lacks %q:\n%s", p, w.stderr.String())
		}
	}
}

// R1
func TestHelpIsExitZeroOnStdout(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, Main([]string{"-h"}, w.env()))
	if !strings.Contains(w.stdout.String(), "usage: qa-ready-wait-rules") || w.stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", w.stdout.String(), w.stderr.String())
	}
}

// R1
func TestUsageErrorsAreExitTwoWithTheUsage(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--root"}, {"--scripts"}} {
		w := newWorld(t, nil)
		w.wantExit(ExitCannotRun, Main(args, w.env()))
		w.wantStderr("ERROR: ", "usage: qa-ready-wait-rules")
	}
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--bogus"}, w.env()))
	w.wantStderr("ERROR: unknown argument '--bogus'")
}

// R1: --scripts names the scripts directory and the root is its parent.
func TestScriptsFlagSetsTheRoot(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, Main([]string{"--scripts", filepath.Join(w.root, "scripts")}, w.env()))
}

// R3: a root reached through a symlink is resolved and its path never printed.
func TestSymlinkedRootNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/waits.bash": waitingScript})
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitFailed, Main([]string{"--root", link}, w.env()))
	if all := w.stdout.String() + w.stderr.String(); strings.Contains(all, w.root) || strings.Contains(all, link) {
		t.Fatalf("a checkout path leaked:\n%s", all)
	}
	for _, argv := range w.fake.argvs {
		if argv[0] != w.root {
			t.Fatalf("semgrep ran in %q, want the real root %q", argv[0], w.root)
		}
	}
}

// R3
func TestAnUnresolvableRootIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, Main([]string{"--root", filepath.Join(w.root, "missing")}, w.env()))
	w.wantStderr("ERROR: cannot resolve the repository root")
}

// R4
func TestSemgrepMissingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.versionEr = exec.ErrNotFound
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)")
}

// R4: a semgrep that is there but does not answer says why, and is not called "not found".
func TestSemgrepThatFailsToAnswerSaysWhy(t *testing.T) {
	cases := map[string]struct {
		set  func(*fakeRunner)
		want string
	}{
		"cannot start": {func(f *fakeRunner) { f.versionEr = errors.New("permission denied") }, "semgrep --version could not run: semgrep: permission denied"},
		"exits 1":      {func(f *fakeRunner) { f.version = proc.Result{Code: 1, Stderr: "broken install\n"} }, "semgrep --version exited 1: broken install"},
		"timed out":    {func(f *fakeRunner) { f.version = proc.Result{TimedOut: true} }, "semgrep --version could not run: semgrep did not finish within"},
		"leftover":     {func(f *fakeRunner) { f.version = proc.Result{Leftover: true} }, "semgrep --version could not run: semgrep left a process holding its output open"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			c.set(w.fake)
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: " + c.want)
			if strings.Contains(w.stderr.String(), "not found") {
				t.Fatalf("a semgrep that exists was called not found:\n%s", w.stderr.String())
			}
		})
	}
}

// R2: a semgrep scan that times out or leaves a process behind is exit 2 saying so.
func TestAScanThatTimesOutOrLeavesAProcessIsExitTwo(t *testing.T) {
	for name, res := range map[string]proc.Result{
		"timed out": {TimedOut: true},
		"leftover":  {Leftover: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.fake.scan = func([]string) proc.Result { return res }
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: semgrep ")
		})
	}
}

// R9, R3: a path outside the root, or one that is absolute, is not judged under another name.
func TestPathsOutsideTheRootAreExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	other := filepath.Join(t.TempDir(), "x.bash")
	if err := os.WriteFile(other, []byte("echo x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{other, "../x.bash", "a/../../x.bash"} {
		w.stderr.Reset()
		w.wantExit(ExitCannotRun, w.run(p))
		w.wantStderr("ERROR: ", p, "outside the repository root")
	}
}

// R9, R3: an absolute path inside the root is judged and shown relative to it.
func TestAnAbsolutePathInsideTheRootIsShownRelative(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/waits.bash": waitingScript})
	w.wantExit(ExitFailed, w.run(filepath.Join(w.root, "scripts", "waits.bash")))
	if !strings.Contains(w.stdout.String(), "scripts/waits.bash:4: ready-wait-ignores-child-exit") || strings.Contains(w.stdout.String(), w.root) {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// R5
func TestAMissingRuleOrFixtureIsExitTwoNamingIt(t *testing.T) {
	for _, rel := range []string{".semgrep/ready-wait.yml", ".semgrep/ready-wait.py", ".semgrep/ready-wait.bash"} {
		w := newWorld(t, map[string]string{rel: ""})
		w.wantExit(ExitCannotRun, w.run())
		w.wantStderr("ERROR: ", rel)
	}
}

// R6
func TestAPythonRuleThatFailsItsFixtureIsExitOneWithSemgrepsOutput(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.test = proc.Result{Code: 1, Stdout: "SEMGREP-OUT\n", Stderr: "SEMGREP-ERR\n"}
	w.wantExit(ExitFailed, w.run())
	w.wantStderr("ERROR: .semgrep/ready-wait.yml failed its fixture .semgrep/ready-wait.py:", "SEMGREP-OUT\n", "SEMGREP-ERR\n")
	if len(w.fake.semgrepScans()) != 0 {
		t.Fatal("a scan ran after the rule failed its proof")
	}
	for _, argv := range w.fake.argvs {
		if slices.Contains(argv, "--test") {
			for _, want := range []string{"--metrics=off", semgrepreport.TimeoutFlag, "--disable-version-check", "--config", filepath.Join(w.root, ".semgrep/ready-wait.yml"), filepath.Join(w.root, ".semgrep/ready-wait.py")} {
				if !slices.Contains(argv, want) {
					t.Fatalf("semgrep --test lacks %q: %v", want, argv)
				}
			}
		}
	}
}

// R7
func TestABashRuleThatFailsItsFixtureIsExitOne(t *testing.T) {
	cases := map[string]string{
		"a mark the rule misses":    "# ruleid: ready-wait-ignores-child-exit\necho not a loop\n",
		"a finding with no mark":    waitingScript,
		"a fixture marking nothing": "echo hi\n",
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{".semgrep/ready-wait.bash": fixture})
			w.wantExit(ExitFailed, w.run())
			w.wantStderr("ERROR: the bash rule failed its fixture .semgrep/ready-wait.bash: the fixture marks lines")
		})
	}
}

// R8
func TestDefaultScopeIsTrackedPythonLessSemgrepAndTrackedShell(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	scans := w.fake.semgrepScans()
	if len(scans) != 1 {
		t.Fatalf("scans: %v", scans)
	}
	if got := handed(scans[0]); !slices.Equal(got, []string{"helpers/mod.py"}) {
		t.Fatalf("handed to semgrep: %v", got)
	}
	if !strings.Contains(w.stdout.String(), "scanned: 1 Python file(s) with semgrep, 1 shell file(s) with the bash rule\n") {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// R8
func TestDefaultScopeWithNothingToCheckIsExitTwo(t *testing.T) {
	cases := map[string]map[string]string{
		"no shell":                 {"scripts/tool.bash": ""},
		"no python":                {"helpers/mod.py": "", ".semgrep/ignored-fixture.py": ""},
		"only the fixtures python": {"helpers/mod.py": ""},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, files)
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ")
		})
	}
}

// R8
func TestDiscoveryFailingIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	if err := os.RemoveAll(filepath.Join(w.root, ".git")); err != nil {
		t.Fatal(err)
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: ", "cannot list the files git would publish")
}

// R9
func TestExplicitPathsAreClassifiedByNameThenShebang(t *testing.T) {
	w := newWorld(t, map[string]string{
		"x/a.py":       "x = 1\n",
		"x/b.bash":     "echo b\n",
		"x/c.sh":       "echo c\n",
		"x/py-tool":    "#!/usr/bin/env python3\nx = 1\n",
		"x/sh-tool":    "#!/bin/sh\necho s\n",
		"x/no-shebang": "echo plain\n",
	})
	w.wantExit(ExitOK, w.run("x/a.py", "x/b.bash", "x/c.sh", "x/py-tool", "x/sh-tool", "x/no-shebang", "x/a.py"))
	scans := w.fake.semgrepScans()
	if len(scans) != 1 || !slices.Equal(handed(scans[0]), []string{"x/a.py", "x/py-tool"}) {
		t.Fatalf("scans: %v", scans)
	}
	if !strings.Contains(w.stdout.String(), "scanned: 2 Python file(s) with semgrep, 4 shell file(s) with the bash rule\n") {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// R9, R13
func TestExplicitShellOnlyDoesNotStartSemgrepForTheScan(t *testing.T) {
	w := newWorld(t, map[string]string{"x/b.bash": "echo b\n"})
	w.wantExit(ExitOK, w.run("x/b.bash"))
	if len(w.fake.semgrepScans()) != 0 {
		t.Fatalf("scans: %v", w.fake.semgrepScans())
	}
	w.wantNoStderr()
}

func (w *world) wantNoStderr() {
	w.t.Helper()
	if w.stderr.Len() != 0 {
		w.t.Fatalf("stderr:\n%s", w.stderr.String())
	}
}

// R9
func TestAnUnreadablePathIsExitTwoNamingIt(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitCannotRun, w.run("nope/missing.bash"))
	w.wantStderr("ERROR: ", "nope/missing.bash")
}

// R9: a path under .semgrep is judged when named.
func TestExplicitScopeIsNotFilteredByTheFixtureTree(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run(".semgrep/ignored-fixture.py"))
	scans := w.fake.semgrepScans()
	if len(scans) != 1 || !slices.Equal(handed(scans[0]), []string{".semgrep/ignored-fixture.py"}) {
		t.Fatalf("scans: %v", scans)
	}
}

// R10
func TestTheScanIsRunInTheRootWithTheDocumentedArguments(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	argv := w.fake.semgrepScans()[0]
	if argv[0] != w.root {
		t.Fatalf("ran in %q", argv[0])
	}
	want := []string{"semgrep", "--metrics=off", "--disable-version-check", "--config", filepath.Join(w.root, ".semgrep/ready-wait.yml"), "--no-rewrite-rule-ids", "--scan-unknown-extensions", "--json", semgrepreport.TimeoutFlag, "--quiet", "helpers/mod.py"}
	if !slices.Equal(argv[1:], want) {
		t.Fatalf("argv %v\nwant %v", argv[1:], want)
	}
}

// R10
func TestSemgrepFailingOrSayingNothingIsExitTwo(t *testing.T) {
	cases := map[string]proc.Result{
		"exit 2":       {Code: 2, Stderr: "boom\n", Stdout: "{}"},
		"empty report": {Code: 0},
		"not json":     {Code: 0, Stdout: "nope"},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.fake.scan = func([]string) proc.Result { return res }
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: ")
			if name == "exit 2" {
				w.wantStderr("semgrep failed on the Python files (exit 2):", "boom")
			}
		})
	}
}

// R11
func TestSemgrepParseErrorsAreExitTwoListingThem(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(argv []string) proc.Result {
		return reportFor(t, handed(argv), nil, []map[string]string{{"path": "helpers/mod.py", "message": "syntax error"}})
	}
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep could not parse some Python files, so no rule ran on them:", "    helpers/mod.py: syntax error")
}

// R12
func TestAFileSemgrepDidNotScanIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func([]string) proc.Result { return reportFor(t, nil, nil, nil) }
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: semgrep did not scan Python files it was handed:", "    helpers/mod.py")
}

// R12
func TestAReportWithoutScannedPathsIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func([]string) proc.Result { return proc.Result{Stdout: `{"results": [], "errors": []}`} }
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: ", "paths.scanned")
}

// R12: semgrep naming a file with a leading ./ is still that file.
func TestScannedPathsWithADotSlashPrefixMatch(t *testing.T) {
	w := newWorld(t, nil)
	w.fake.scan = func(argv []string) proc.Result {
		named := make([]string, 0, len(argv))
		for _, f := range handed(argv) {
			named = append(named, "./"+f)
		}
		return reportFor(t, named, nil, nil)
	}
	w.wantExit(ExitOK, w.run())
}

// R13, R25, R26
func TestFindingsFromBothHalvesAreSortedAndSummarised(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/waits.bash": waitingScript})
	w.fake.scan = func(argv []string) proc.Result {
		result := jsonResult{CheckID: "ready-wait-ignores-child-exit", Path: "./helpers/mod.py"}
		result.Start.Line = 7
		return reportFor(t, handed(argv), []jsonResult{result}, nil)
	}
	w.wantExit(ExitFailed, w.run())
	want := "scanned: 1 Python file(s) with semgrep, 2 shell file(s) with the bash rule\n" +
		"helpers/mod.py:7: ready-wait-ignores-child-exit\n" +
		"scripts/waits.bash:4: ready-wait-ignores-child-exit\n" +
		"ready-wait rule: 2 finding(s) in 2 file(s). See CLAUDE/QA.md \"ready-wait-ignores-child-exit\".\n" +
		"passed: 1 failed: 2\n"
	if w.stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", w.stdout.String(), want)
	}
	w.wantNoStderr()
}

// R25, R26: two findings in one file are two findings in one file; the same finding twice is one.
func TestFindingsAreCountedPerFindingAndPerFile(t *testing.T) {
	two := waitingScript + "w &\np=$!\nuntil [ -e s ]; do\n\tsleep 1\ndone\n"
	w := newWorld(t, map[string]string{"scripts/waits.bash": two})
	w.fake.scan = func(argv []string) proc.Result {
		a := jsonResult{CheckID: "r", Path: "helpers/mod.py"}
		a.Start.Line = 3
		return reportFor(t, handed(argv), []jsonResult{a, a}, nil)
	}
	w.wantExit(ExitFailed, w.run())
	if !strings.Contains(w.stdout.String(), "ready-wait rule: 3 finding(s) in 2 file(s).") {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// R26
func TestACleanRunPrintsPassedAndExitsZero(t *testing.T) {
	w := newWorld(t, nil)
	w.wantExit(ExitOK, w.run())
	want := "scanned: 1 Python file(s) with semgrep, 1 shell file(s) with the bash rule\npassed: 2 failed: 0\n"
	if w.stdout.String() != want {
		t.Fatalf("stdout:\n%s", w.stdout.String())
	}
}

// R24 at the file level: a shell file that cannot be read as bash is exit 2 naming it.
func TestAShellFileThatCannotBeParsedIsExitTwoNamingIt(t *testing.T) {
	for name, text := range map[string]string{
		"not UTF-8":    "echo \xe9\xff\n" + waitingScript,
		"a bad syntax": "if true; then\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{"scripts/broken.bash": text})
			w.wantExit(ExitCannotRun, w.run())
			w.wantStderr("ERROR: scripts/broken.bash: cannot parse as bash")
		})
	}
}

// R14: a fixture that does not parse is exit 2, not a pass.
func TestABashFixtureThatCannotBeParsedIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{".semgrep/ready-wait.bash": "if true; then\n"})
	w.wantExit(ExitCannotRun, w.run())
	w.wantStderr("ERROR: the bash rule cannot read its fixture .semgrep/ready-wait.bash")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// R2
func TestAFailedWriteToStdoutIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	env := w.env()
	env.Stdout = failingWriter{}
	w.wantExit(ExitCannotRun, Main([]string{"--root", w.root}, env))
	w.wantStderr("ERROR: cannot write to stdout: disk full")
}
