package changed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// runWorld is a scratch git repository (the root) plus a separate scripts directory carrying
// the real tables, a fake gate table with two suites, and stub gates.
type runWorld struct {
	t       *testing.T
	root    string
	scripts string
	runner  *scriptedRunner
	out     strings.Builder
	errOut  strings.Builder
	missing map[string]bool
	env     map[string]string
}

func newRunWorld(t *testing.T) *runWorld {
	t.Helper()
	w := &runWorld{t: t, root: scratchRepo(t), scripts: filepath.Join(t.TempDir(), "scripts"), runner: &scriptedRunner{replies: map[string]proc.Result{}, vcs: true, semgrepReport: true}, missing: map[string]bool{}, env: map[string]string{}}
	for _, name := range []string{"qa-routing.tsv", "qa-trees.tsv"} {
		writeFile(t, w.scripts, name, readReal(t, name))
	}
	writeFile(t, w.scripts, "qa-gates.tsv", "gate\tone\tscripts/test-one.bash\tone failed\tcount\ngate\ttwo\tscripts/test-two.bash\ttwo failed\tcount\n")
	writeFile(t, w.scripts, "test-one.bash", "#!/usr/bin/bash\nkept.bash\n")
	writeFile(t, w.scripts, "test-two.bash", "#!/usr/bin/bash\nkept.bash\n")
	writeFile(t, w.scripts, "go-binary-suites.tsv", "frz\tqa-go\n")
	writeFile(t, w.scripts, "bash-limits.allowlist", "# none\n")
	for _, suite := range []string{"test-one.bash", "test-two.bash"} {
		w.runner.replies["bash "+filepath.Join(w.scripts, suite)] = proc.Result{Stdout: "passed: 1 failed: 0\n"}
	}
	return w
}

func (w *runWorld) main(args ...string) ExitCode {
	w.t.Helper()
	w.out.Reset()
	w.errOut.Reset()
	env := Env{
		Getenv: func(k string) string { return w.env[k] },
		Stdout: &w.out,
		Stderr: &w.errOut,
		Runner: w.runner,
		LookPath: func(tool string) error {
			if w.missing[tool] {
				return errors.New("not found")
			}
			return nil
		},
		Cwd: w.root,
	}
	return Main(context.Background(), append([]string{"--scripts", w.scripts, "--root", w.root}, args...), env)
}

// changeKept makes kept.bash a changed shell file in the scratch repository.
func (w *runWorld) changeKept() {
	writeFile(w.t, w.root, "kept.bash", "#!/usr/bin/bash\n: changed\n")
}

func lines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func TestAllChecksPassPrintsTheContractLines(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = proc.Result{Stdout: "case\npassed: 12 failed: 0\n"}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "passed: 3 failed: 0\n"}
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	want := []string{
		"plan: 1 changed file(s) vs HEAD",
		"check toolchain",
		"check bash-syntax: kept.bash",
		"check shellcheck: kept.bash",
		"check shell-directives: kept.bash",
		"check semgrep: kept.bash",
		"check bash-limits",
		"suite scripts/test-one.bash: names kept.bash",
		"suite scripts/test-two.bash: names kept.bash",
		"✓ toolchain: QA tool versions match .qa-versions",
		"✓ bash-syntax: 1 file(s)",
		"✓ shellcheck: 1 file(s)",
		"✓ shell-directives: 1 file(s)",
		"✓ semgrep: 1 file(s)",
		"✓ bash-limits: whole gate (tooling is Go; allowlist ratchet)",
		"✓ suite test-one.bash: passed: 12 (names kept.bash)",
		"✓ suite test-two.bash: passed: 3 (names kept.bash)",
		"✓ qa-changed passed: 1 changed file(s). Run ./scripts/qa-all.bash before pushing.",
	}
	if got := lines(w.out.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("stdout:\n%s", strings.Join(got, "\n"))
	}
	if w.errOut.String() != "" {
		t.Fatalf("stderr: %q", w.errOut.String())
	}
}

func TestChecksRunInOrderAndEveryOneRunsAfterAFailure(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["shellcheck"] = proc.Result{Stdout: "SC2086 bad\n", Code: 1}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = proc.Result{Stdout: "passed: 5\n"}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "boom\n", Stderr: "", Code: 1}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	out := w.out.String()
	for _, want := range []string{
		"✗ shellcheck: 1 file(s)\n",
		"✓ semgrep: 1 file(s)\n",
		"✓ bash-limits:",
		"✓ suite test-one.bash: passed: 5 (names kept.bash)\n",
		"✗ suite test-two.bash: (names kept.bash)\n",
		"✗ qa-changed FAILED: shellcheck suite test-two.bash\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "qa-changed passed") {
		t.Fatal("a failed run must not print the pass line")
	}
	for _, want := range []string{"SC2086 bad\n", "boom\n"} {
		if !strings.Contains(w.errOut.String(), want) {
			t.Errorf("stderr lacks the failing check's detail %q: %q", want, w.errOut.String())
		}
	}
	if strings.Contains(w.out.String(), "SC2086") {
		t.Fatal("the failing check's output belongs on stderr")
	}
	argv := w.runner.argv()
	if idx(argv, "shellcheck") > idx(argv, "bash "+filepath.Join(w.scripts, "qa-bash-limits.bash")) {
		t.Fatalf("order: %v", argv)
	}
}

func nonVCS(calls []proc.Cmd) int {
	n := 0
	for _, c := range calls {
		if c.Argv[0] != "gi"+"t" {
			n++
		}
	}
	return n
}

func idx(list []string, prefix string) int {
	for i, s := range list {
		if strings.HasPrefix(s, prefix) {
			return i
		}
	}
	return -1
}

func TestAGateThatCannotRunIsNotAnOrdinaryFailure(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash "+filepath.Join(w.scripts, "qa-bash-limits.bash")] = proc.Result{Stdout: "no go\n", Code: 2}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	out := w.out.String()
	if !strings.Contains(out, "✗ bash-limits: whole gate (tooling is Go; allowlist ratchet) — could not run (exit 2)\n") || !strings.Contains(out, "✗ qa-changed FAILED: bash-limits\n") {
		t.Fatalf("stdout:\n%s", out)
	}
	// A failed check outranks a check that could not run.
	w.runner.replies["shellcheck"] = proc.Result{Code: 1}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("a failure outranks cannot-run: exit %d", code)
	}
	if !strings.Contains(w.out.String(), "✗ qa-changed FAILED: shellcheck bash-limits\n") {
		t.Fatalf("both are listed:\n%s", w.out.String())
	}
}

// The reasons a suite was picked are listed in the order of the changed files, not sorted by
// the text that matched (the bash sorted by matched text; SPEC-qa-changed.md B27). Here the
// matched text sorts a.bash before b.bash, but the changed files sort b.bash before dir/a.bash.
func TestSuiteReasonsFollowTheOrderOfTheChangedFiles(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.scripts, "test-one.bash", "#!/usr/bin/bash\nb.bash a.bash\n")
	writeFile(t, w.root, "b.bash", "#!/usr/bin/bash\n")
	writeFile(t, w.root, "dir/a.bash", "#!/usr/bin/bash\n")
	if code := w.main("--plan"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	want := "suite scripts/test-one.bash: names b.bash; names dir/a.bash\n"
	if !strings.Contains(w.out.String(), want) {
		t.Fatalf("want %q in:\n%s", want, w.out.String())
	}
}

// A suite whose harness died before its summary can still exit 0; with no case count it has
// proved nothing, so it is "could not run", never a pass (SPEC-qa-changed.md B29).
func TestASuiteThatExitsZeroWithoutACaseCountCouldNotRun(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = proc.Result{Stdout: "set up, then nothing\n"}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "passed: 3 failed: 0\n"}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d", code, ExitCannotRun)
	}
	out := w.out.String()
	for _, want := range []string{
		"✗ suite test-one.bash: (names kept.bash) — could not run (exit 2)\n",
		"✓ suite test-two.bash: passed: 3 (names kept.bash)\n",
		"✗ qa-changed FAILED: suite test-one.bash\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(w.errOut.String(), "printed no 'passed: <n>' line") || !strings.Contains(w.errOut.String(), "set up, then nothing") {
		t.Fatalf("stderr must say why and keep the output: %q", w.errOut.String())
	}
}

// A suite that counted no cases ran nothing: also could not run (SPEC-qa-changed.md B31).
func TestASuiteThatCountedZeroCasesCouldNotRun(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = proc.Result{Stdout: "passed: 0 failed: 0\n"}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "passed: 3 failed: 0\n"}
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d", code, ExitCannotRun)
	}
	if !strings.Contains(w.out.String(), "✗ suite test-one.bash: (names kept.bash) — could not run (exit 2)\n") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	if !strings.Contains(w.errOut.String(), "counted 0 passed cases") {
		t.Fatalf("stderr must say why: %q", w.errOut.String())
	}
}

// A count line that reports failures is a failed suite although it exited 0 (B32).
func TestASuiteWhoseCountLineReportsFailuresFailsDespiteExitZero(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-one.bash")] = proc.Result{Stdout: "passed: 3 failed: 2\n"}
	w.runner.replies["bash "+filepath.Join(w.scripts, "test-two.bash")] = proc.Result{Stdout: "passed: 3 failed: 0\n"}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d, want %d", code, ExitFailed)
	}
	out := w.out.String()
	if !strings.Contains(out, "✗ suite test-one.bash: (names kept.bash)\n") || !strings.Contains(out, "✗ qa-changed FAILED: suite test-one.bash\n") {
		t.Fatalf("stdout:\n%s", out)
	}
	if !strings.Contains(w.errOut.String(), "2 failed") {
		t.Fatalf("stderr must say why: %q", w.errOut.String())
	}
}

// A tool that exited 0 but left a child holding its output open is a failure that keeps the
// output it wrote and says what it left behind.
func TestACheckThatLeavesAChildBehindFailsAndKeepsItsOutput(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash -n"] = proc.Result{Stdout: "captured\n", Leftover: true}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "captured\n") || !strings.Contains(w.errOut.String(), "left a process running") {
		t.Fatalf("stderr: %q", w.errOut.String())
	}
}

func TestAnExitOf2FromAPlainToolIsAFailureNotACannotRun(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.replies["bash -n"] = proc.Result{Stdout: "syntax error\n", Code: 2}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
}

func TestACheckThatOutlivesItsTimeoutIsKilledAndReported(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.env["QA_CHANGED_TIMEOUT"] = "50ms"
	w.runner.onRun = func(ctx context.Context, c proc.Cmd) {
		if len(c.Argv) > 1 && strings.HasSuffix(c.Argv[1], "qa-toolchain.bash") {
			<-ctx.Done()
		}
	}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	out := w.out.String()
	if !strings.Contains(out, "✗ toolchain: QA tool versions match .qa-versions — timed out after 50ms\n") || !strings.Contains(out, "✓ shellcheck") {
		t.Fatalf("stdout:\n%s", out)
	}
}

// A semgrep that was killed at the timeout wrote no report; reading the missing report would add
// a second, misleading error to the timeout line.
func TestAKilledSemgrepDoesNotAlsoComplainAboutItsReport(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.runner.semgrepReport = false
	w.runner.replies["semgrep"] = proc.Result{Code: -1, TimedOut: true}
	if code := w.main(); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(w.errOut.String(), "cannot read semgrep's report") {
		t.Fatalf("stderr: %q", w.errOut.String())
	}
}

func TestZeroDisablesTheTimeout(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.env["QA_CHANGED_TIMEOUT"] = "0"
	w.runner.onRun = func(ctx context.Context, _ proc.Cmd) {
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			t.Error("a deadline was set")
		}
	}
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestInterruptStopsTheRunAndKeepsTheStatus(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	ctx, cancel := context.WithCancel(context.Background())
	w.runner.onRun = func(_ context.Context, c proc.Cmd) {
		if c.Argv[0] != "gi"+"t" {
			cancel()
		}
	}
	env := Env{Getenv: func(string) string { return "" }, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }}
	code := Main(ctx, []string{"--scripts", w.scripts, "--root", w.root}, env)
	if code != ExitInterrupted {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(w.out.String(), "qa-changed passed") || strings.Contains(w.out.String(), "✓ shellcheck") {
		t.Fatalf("the run went on after the interrupt:\n%s", w.out.String())
	}
}

func TestPreflightNamesTheFirstMissingToolBeforeAnyCheckRuns(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.missing["shellcheck"] = true
	w.missing["semgrep"] = true
	if code := w.main(); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	want := "ERROR: shellcheck not found — a planned check needs it.\n  Deploy it: ansible-playbook playbooks/imports/play-devtools.yml\n  Do NOT install it by hand.\n"
	if w.errOut.String() != want {
		t.Fatalf("stderr %q", w.errOut.String())
	}
	if nonVCS(w.runner.calls) > 0 || strings.Contains(w.out.String(), "✓") {
		t.Fatalf("a check ran before the preflight finished: %v", w.runner.argv())
	}
	if !strings.Contains(w.out.String(), "check shellcheck: kept.bash") {
		t.Fatal("the plan is printed before the preflight")
	}
}

func TestPreflightKnowsEveryToolAChecksNeeds(t *testing.T) {
	cases := map[string]string{
		"semgrep":          "play-python.yml",
		"ruff":             "play-python.yml",
		"ansible-playbook": "sudo dnf install ansible",
		"node":             "play-nvm-install.yml",
		"python3":          "play-python.yml",
		"go":               "play-golang.yml",
		"golangci-lint":    "play-python.yml",
		"bash":             "bash",
	}
	for tool, fix := range cases {
		var stderr strings.Builder
		s := &session{env: Env{LookPath: func(name string) error {
			if name == tool {
				return errors.New("no")
			}
			return nil
		}, Stderr: &stderr}, root: t.TempDir(), plan: &plan{checks: map[string]*checkPlan{}}}
		for _, d := range checkDefs {
			if d.id != "eslint" {
				s.plan.add(d.id, "x", "")
			}
		}
		err := s.preflight()
		if err == nil {
			t.Errorf("%s: not flagged", tool)
			continue
		}
		if !strings.Contains(stderr.String(), "ERROR: "+tool+" not found") || !strings.Contains(stderr.String(), fix) {
			t.Errorf("%s: %q", tool, stderr.String())
		}
	}
}

func TestPreflightEslintIsAPathUnderExtensions(t *testing.T) {
	var stderr strings.Builder
	s := &session{env: Env{LookPath: func(string) error { return nil }, Stderr: &stderr}, root: t.TempDir(), plan: &plan{checks: map[string]*checkPlan{}}}
	s.plan.add("eslint", "extensions/a/x.js", "extensions")
	if err := s.preflight(); err == nil || stderr.String() != "ERROR: eslint for extensions/ is not set up (cd extensions && npm ci).\n" {
		t.Fatalf("%v %q", err, stderr.String())
	}
	bin := filepath.Join(s.root, "extensions", "node_modules", ".bin", "eslint")
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.preflight(); err == nil {
		t.Fatal("a file that is not executable is not set up")
	}
	if err := os.Chmod(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.preflight(); err != nil {
		t.Fatalf("an executable eslint: %v", err)
	}
}

func TestPlanOnlyRunsNothingAndNeedsNoTools(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	w.missing["shellcheck"] = true
	before := len(w.runner.calls)
	if code := w.main("--plan"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if strings.Contains(w.out.String(), "✓") || strings.Contains(w.out.String(), "passed:") {
		t.Fatalf("--plan ran something:\n%s", w.out.String())
	}
	for _, c := range w.runner.calls[before:] {
		if c.Argv[0] != "git" && c.Argv[0] != "go" {
			t.Fatalf("--plan started %v", c.Argv)
		}
	}
}

func TestPlanListsSkippedNotesAndUncovered(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.root, "untracked/x.js", "x\n")
	writeFile(t, w.root, "helpers/a/lonely.py", "x = 1\n")
	writeFile(t, w.root, "misc/other.txt", "x\n")
	writeFile(t, w.root, "kept.bash", "#!/usr/bin/bash\n: changed\n")
	if code := w.main("--plan"); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	got := lines(w.out.String())
	for _, want := range []string{
		"skipped (excluded tree): untracked/x.js",
		"note: helpers/a/lonely.py: no mirrored unit test (tests/helpers/a/test_lonely.py)",
		"⚠ not covered by qa-changed: misc/other.txt",
	} {
		if !contains(got, want) {
			t.Errorf("plan lacks %q:\n%s", want, w.out.String())
		}
	}
}

func contains(list []string, want string) bool {
	return idx(list, want) >= 0
}

func TestUncoveredFilesWarnWithoutChangingTheStatus(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.root, "misc/other.txt", "x\n")
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	out := w.out.String()
	if !strings.Contains(out, "⚠ 1 changed file(s) not covered by qa-changed — qa-all.bash is the only check\n") || !strings.Contains(out, "✓ qa-changed passed: 1 changed file(s).") {
		t.Fatalf("stdout:\n%s", out)
	}
}

func TestNothingChangedIsExitZeroWithTheNoOpLine(t *testing.T) {
	w := newRunWorld(t)
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if w.out.String() != "qa-changed: no changed files (vs HEAD, plus untracked) — nothing to check.\n" {
		t.Fatalf("stdout %q", w.out.String())
	}
}

func TestAnExplicitListOfMissingFilesIsExit2NotNothingToCheck(t *testing.T) {
	w := newRunWorld(t)
	if code := w.main("--files", "no/such/file.bash"); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d", code, ExitCannotRun)
	}
	if strings.Contains(w.out.String(), "nothing to check") || !strings.Contains(w.errOut.String(), "names no existing file") {
		t.Fatalf("stdout %q stderr %q", w.out.String(), w.errOut.String())
	}
}

func TestExplicitFilesPlanWithoutGitAndWithFlagsAfterThem(t *testing.T) {
	w := newRunWorld(t)
	writeFile(t, w.root, "a.bash", "#!/usr/bin/bash\n")
	if code := w.main("--files", "./a.bash", "a.bash", "--plan"); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	got := lines(w.out.String())
	if got[0] != "plan: 1 changed file(s) (explicit)" || !contains(got, "check bash-syntax: a.bash") {
		t.Fatalf("stdout:\n%s", w.out.String())
	}
	for _, c := range w.runner.calls {
		if c.Argv[0] == "git" && c.Argv[1] == "diff" {
			t.Fatalf("explicit files must not diff: %v", c.Argv)
		}
	}
}

func TestMainRefusesBadInputWithExitTwo(t *testing.T) {
	w := newRunWorld(t)
	cases := map[string]struct {
		args []string
		env  map[string]string
		want string
	}{
		"unknown flag":      {[]string{"--nope"}, nil, "ERROR: unknown argument '--nope'"},
		"base needs a ref":  {[]string{"--base"}, nil, "ERROR: --base needs a ref"},
		"empty files":       {[]string{"--files"}, nil, "ERROR: --files needs at least one file"},
		"bad timeout":       {nil, map[string]string{"QA_CHANGED_TIMEOUT": "soon"}, "QA_CHANGED_TIMEOUT"},
		"file is outside":   {[]string{"--files", "../x"}, nil, "outside the repository"},
		"root not a dir":    {[]string{"--root", "/no/such/dir"}, nil, "ERROR: root /no/such/dir is not a directory"},
		"scripts not found": {[]string{"--scripts", "/no/such/dir"}, nil, "ERROR:"},
	}
	for name, c := range cases {
		w.env = c.env
		if w.env == nil {
			w.env = map[string]string{}
		}
		w.out.Reset()
		w.errOut.Reset()
		env := Env{Getenv: func(k string) string { return w.env[k] }, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }, Cwd: w.root}
		args := append([]string{"--scripts", w.scripts, "--root", w.root}, c.args...)
		if code := Main(context.Background(), args, env); code != ExitCannotRun {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(w.errOut.String(), c.want) {
			t.Errorf("%s: stderr %q", name, w.errOut.String())
		}
	}
}

func TestUsageErrorPrintsTheUsageOnStderrAndHelpOnStdout(t *testing.T) {
	w := newRunWorld(t)
	if code := w.main("--nope"); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "usage: qa-changed") || w.out.String() != "" {
		t.Fatalf("exit %d out %q err %q", code, w.out.String(), w.errOut.String())
	}
	if code := w.main("--help"); code != ExitOK || !strings.HasPrefix(w.out.String(), "usage: qa-changed") || w.errOut.String() != "" {
		t.Fatalf("help: exit %d out %q err %q", code, w.out.String(), w.errOut.String())
	}
}

func TestABadBaseIsExitTwoWithGitsMessage(t *testing.T) {
	w := newRunWorld(t)
	w.runner = &scriptedRunner{replies: map[string]proc.Result{"git diff": {Stderr: "fatal: bad revision 'nosuchref'", Code: 128}}}
	if code := w.main("--base", "nosuchref", "--plan"); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "cannot list changed files vs nosuchref") || !strings.Contains(w.errOut.String(), "bad revision") || strings.Contains(w.out.String(), "nothing to check") {
		t.Fatalf("stderr %q stdout %q", w.errOut.String(), w.out.String())
	}
}

func TestDefaultRootAndScriptsDeriveFromEachOther(t *testing.T) {
	w := newRunWorld(t)
	env := Env{Getenv: func(k string) string {
		if k == "QA_CHANGED_ROOT" {
			return w.root
		}
		return ""
	}, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }, Cwd: "/elsewhere"}
	if code := Main(context.Background(), []string{"--scripts", w.scripts}, env); code != ExitOK {
		t.Fatalf("QA_CHANGED_ROOT with --scripts: exit %d, %q", code, w.errOut.String())
	}
	root, scripts := resolvePaths(options{}, func(string) string { return "" }, "/work")
	if root != "/work" || scripts != "/work/scripts" {
		t.Fatalf("defaults: %s %s", root, scripts)
	}
	root, scripts = resolvePaths(options{scripts: "/repo/scripts"}, func(string) string { return "" }, "/work")
	if root != "/repo" || scripts != "/repo/scripts" {
		t.Fatalf("from scripts: %s %s", root, scripts)
	}
	root, scripts = resolvePaths(options{root: "/r"}, func(string) string { return "/env" }, "/work")
	if root != "/r" || scripts != "/r/scripts" {
		t.Fatalf("flag beats env: %s %s", root, scripts)
	}
}

func TestRunnerFailureOfACheckIsReportedAsAFailure(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	good := w.runner
	w.runner = &scriptedRunner{}
	broken := &mixedRunner{good: good, bad: "shellcheck"}
	env := Env{Getenv: func(string) string { return "" }, Stdout: &w.out, Stderr: &w.errOut, Runner: broken, LookPath: func(string) error { return nil }}
	if code := Main(context.Background(), []string{"--scripts", w.scripts, "--root", w.root}, env); code != ExitFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.errOut.String(), "cannot run shellcheck") {
		t.Fatalf("stderr %q", w.errOut.String())
	}
}

func TestRelativeRootAndScriptsBecomeAbsoluteBeforeAnythingRunsElsewhere(t *testing.T) {
	w := newRunWorld(t)
	w.changeKept()
	t.Chdir(filepath.Dir(w.scripts))
	env := Env{Getenv: func(string) string { return "" }, Stdout: &w.out, Stderr: &w.errOut, Runner: w.runner, LookPath: func(string) error { return nil }}
	if code := Main(context.Background(), []string{"--scripts", "scripts", "--root", w.root}, env); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	for _, c := range w.runner.calls {
		if c.Argv[0] == "semgrep" && !filepath.IsAbs(c.Argv[2]) {
			t.Fatalf("semgrep runs in a scratch directory and was given a relative config: %v", c.Argv)
		}
		if c.Argv[0] == "bash" && c.Argv[1] != "-n" && !filepath.IsAbs(c.Argv[1]) {
			t.Fatalf("a gate script path must be absolute: %v", c.Argv)
		}
	}
}

// mixedRunner fails to start one program and delegates the rest.
type mixedRunner struct {
	good proc.Runner
	bad  string
}

func (m *mixedRunner) Run(ctx context.Context, c proc.Cmd) (proc.Result, error) {
	if c.Argv[0] == m.bad {
		return proc.Result{}, os.ErrPermission
	}
	return m.good.Run(ctx, c)
}
