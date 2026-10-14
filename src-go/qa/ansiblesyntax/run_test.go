package ansiblesyntax

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, code, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, parts ...string) {
	t.Helper()
	wantExit(t, got, ExitCannotRun)
	if !strings.HasPrefix(got.stderr, "ERROR: ") {
		t.Fatalf("stderr does not start with ERROR:\n%s", got.stderr)
	}
	for _, p := range parts {
		if !strings.Contains(got.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, got.stderr)
		}
	}
}

func checkedFiles(rep testReport) []string {
	files := make([]string, 0, len(rep.Results))
	for _, r := range rep.Results {
		files = append(files, r.File)
	}
	return files
}

// S1
func TestHelpPrintsUsageAndExitsZero(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--help")
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "usage: qa-ansible-syntax") || got.stderr != "" || len(w.run.calls) != 0 {
		t.Fatalf("stdout %q stderr %q calls %v", got.stdout, got.stderr, w.run.calls)
	}
}

// S1
func TestBadArgumentsAreAUsageError(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"--root"}, {"--scripts"}, {"extra"}} {
		w := newWorld(t, nil)
		wantStderr(t, w.main(args...), "usage: qa-ansible-syntax")
		w.noReport()
	}
}

// S1: the root is --root, or the parent of --scripts, or the working directory.
func TestRootComesFromTheFlagsOrTheWorkingDirectory(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	wantExit(t, w.main("--root", w.root), ExitOK)
	wantExit(t, w.main("--scripts", filepath.Join(w.root, "scripts")), ExitOK)
	wantExit(t, w.main(), ExitOK)
	for _, c := range w.run.cmds {
		if c.Dir != w.root {
			t.Fatalf("%v ran in %q, not the root", c.Argv, c.Dir)
		}
	}
}

// S1: a relative or untidy root behaves as the absolute path does.
func TestRelativeRootIsMadeAbsolute(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	wantExit(t, w.main("--root", "."), ExitOK)
	wantExit(t, w.main("--root", w.root+"/"), ExitOK)
	for _, c := range w.run.checks() {
		if file := c.Argv[len(c.Argv)-1]; file != filepath.Join(w.root, "p.yml") && file != filepath.Join(w.root, basePlaybook) {
			t.Fatalf("checked %q, not a file under %q", file, w.root)
		}
	}
}

// S2
func TestReportPathDefaultsAndCanBeSet(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.env["QA_JSON_OUT"] = ""
	wantExit(t, w.main(), ExitOK)
	if len(w.wrote) != 1 || w.wrote[0] != "/tmp/qa-ansible-syntax-results.json" {
		t.Fatalf("report written to %v", w.wrote)
	}
}

// S2
func TestUnwritableReportIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.env["QA_JSON_OUT"] = filepath.Join(w.root, "no", "such", "dir", "r.json")
	wantStderr(t, w.main(), "cannot write the report", "r.json")
}

// S3
func TestFailedWriteToAStreamIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	var stderr bytes.Buffer
	env := w.environment(nil, &stderr)
	env.Stdout = failingWriter{}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(stderr.String(), "cannot write to stdout") {
		t.Fatalf("exit %d stderr %q", code, stderr.String())
	}
	env = w.environment(&bytes.Buffer{}, nil)
	env.Stderr = failingWriter{}
	if code := Main([]string{"--nope"}, env); code != ExitCannotRun {
		t.Fatalf("exit %d with a broken stderr", code)
	}
}

// S4
func TestEngineThatCannotRunIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"missing":   func(f *fakeRun) { f.missing = "ansible-playbook" },
		"exit code": func(f *fakeRun) { f.versionCode = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.addPlaybook("p.yml")
			set(w.run)
			wantStderr(t, w.main(), "ansible-playbook not installed", "run.bash")
			w.noReport()
		})
	}
}

// S5
func TestMissingConfigIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"ansible.cfg": ""})
	w.addPlaybook("p.yml")
	wantStderr(t, w.main(), "ansible.cfg")
	w.noReport()
	if len(w.run.checks()) != 0 {
		t.Fatal("playbooks were checked without a config")
	}
}

// S5, S10
func TestEveryCheckPinsTheConfigAndTheWorkingDirectory(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.addPlaybook("playbooks/imports/b.yml")
	wantExit(t, w.main(), ExitOK)
	checks := w.run.checks()
	if len(checks) != 3 {
		t.Fatalf("%d checks", len(checks))
	}
	for _, c := range checks {
		if c.Dir != w.root || !slices.Contains(c.Env, "ANSIBLE_CONFIG="+filepath.Join(w.root, "ansible.cfg")) || !slices.Contains(c.Env, "ANSIBLE_NOCOLOR=1") {
			t.Fatalf("check not pinned: dir %q env %v", c.Dir, c.Env)
		}
	}
	for _, c := range w.run.cmds {
		if c.Argv[0] == "ansible-config" && (c.Dir != w.root || !slices.Contains(c.Env, "ANSIBLE_CONFIG="+filepath.Join(w.root, "ansible.cfg"))) {
			t.Fatalf("config probe not pinned: %v", c)
		}
	}
}

// S6
func TestMissingPasswordFileIsExitTwoNotManyFailures(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.addPlaybook("b.yml")
	w.setting(passwordSetting, filepath.Join(w.root, "absent"))
	got := w.main()
	wantStderr(t, got, "password file", filepath.Join(w.root, "absent"), "exist", "never reads")
	if strings.Contains(got.stdout, "✗") || len(w.run.checks()) != 0 {
		t.Fatalf("playbooks were judged: stdout %q", got.stdout)
	}
	w.noReport()
}

// S6: a directory is not a file; a present file is enough, and it is never opened.
func TestPasswordFileMustBeAFileAndIsOnlyStatted(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.setting(passwordSetting, w.root)
	wantStderr(t, w.main(), "password file")
	w.write("present", "any text\n")
	w.setting(passwordSetting, filepath.Join(w.root, "present"))
	wantExit(t, w.main(), ExitOK)
	if err := os.Chmod(filepath.Join(w.root, "present"), 0); err != nil {
		t.Fatal(err)
	}
	wantExit(t, w.main(), ExitOK)
}

// S6: the engine loads the file and rejects an empty one at every playbook, so an empty file is
// the same exit 2 as an absent one.
func TestEmptyPasswordFileIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.write("empty", "")
	w.setting(passwordSetting, filepath.Join(w.root, "empty"))
	wantStderr(t, w.main(), "password file", "empty")
	if len(w.run.checks()) != 0 {
		t.Fatal("playbooks were checked")
	}
}

// S6: any configured password file counts, not only the vault's; none configured needs none.
func TestEveryConfiguredPasswordFileIsRequired(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.setting("BECOME_PASSWORD_FILE", filepath.Join(w.root, "absent"))
	wantStderr(t, w.main(), "BECOME_PASSWORD_FILE")
	w.setting("BECOME_PASSWORD_FILE", "")
	wantExit(t, w.main(), ExitOK)
	w.setting("ANY_ERRORS_FATAL", true)
	wantExit(t, w.main(), ExitOK)
}

// S6: a config dump that cannot be had or read is the run failing, not a pass.
func TestUnreadableConfigDumpIsExitTwo(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"exit":    func(f *fakeRun) { f.configCode = 1 },
		"missing": func(f *fakeRun) { f.missing = "ansible-config" },
		"not json": func(f *fakeRun) {
			f.configDump = "{nope"
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.addPlaybook("a.yml")
			set(w.run)
			wantStderr(t, w.main(), "ansible-config")
			w.noReport()
		})
	}
}

// S7
func TestAPlaybookHasAPlayOrImportsOne(t *testing.T) {
	w := newWorld(t, nil)
	w.addFile("a.yml", "---\n- hosts: all\n")
	w.addFile("b.yaml", "- import_playbook: a.yml\n")
	w.addFile("c.yml", "  -   hosts: x\n")
	w.addFile("tasks.yml", "- name: t\n  hosts: nope\n")
	w.addFile("comment.yml", "# - hosts: all\n")
	w.addFile("vars.yml", "hosts: [a]\n")
	w.addFile("d.txt", "- hosts: all\n")
	wantExit(t, w.main(), ExitOK)
	if got := checkedFiles(w.report()); !slices.Equal(got, []string{"a.yml", "b.yaml", "c.yml", basePlaybook}) {
		t.Fatalf("checked %v", got)
	}
}

// S7
func TestExcludedTreesAreNotPlaybooks(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.addPlaybook("untracked/scratch/q.yml")
	w.addPlaybook("deep/.git/r.yml")
	wantExit(t, w.main(), ExitOK)
	if got := checkedFiles(w.report()); !slices.Equal(got, []string{"p.yml", basePlaybook}) {
		t.Fatalf("checked %v", got)
	}
}

// S7 (departure D1): a file git would not publish is not read, whatever its text.
func TestFilesGitWouldNotPublishAreNotChecked(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.write("ignored.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	wantExit(t, w.main(), ExitOK)
	if got := checkedFiles(w.report()); !slices.Equal(got, []string{"p.yml", basePlaybook}) {
		t.Fatalf("checked %v", got)
	}
}

// S7
func TestUnreadablePlaybookCandidateIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.run.published = append(w.run.published, "gone.yml")
	wantStderr(t, w.main(), "gone.yml")
	w.noReport()
}

// S8
func TestNoPlaybooksIsAnAbortNotAPass(t *testing.T) {
	w := newWorld(t, map[string]string{basePlaybook: ""})
	w.addFile("tasks.yml", "- name: t\n")
	wantStderr(t, w.main(), "found 0 playbooks", "discovery is broken")
	w.noReport()
}

// S9
func TestTrackedPlaybookOutsideThePopulationIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("p.yml")
	w.addFile("playbooks/tasks-only.yml", "- name: not a play\n")
	wantStderr(t, w.main(), "playbooks/tasks-only.yml", "not in the population", "src-go/qa/discovery")
	w.noReport()
	if len(w.run.checks()) != 0 {
		t.Fatal("playbooks were checked on a population known to be short")
	}
}

// S10
func TestAFailingPlaybookIsReportedWithTheEnginesStderr(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.addFile("b.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	got := w.main()
	wantExit(t, got, ExitFailed)
	wantOut := "✗ ansible-syntax: b.yml\nERROR! bad syntax in " + filepath.Join(w.root, "b.yml") + "\n" +
		"✗ ansible-syntax: 1/3 playbooks failed → " + w.out + "\n"
	if got.stdout != wantOut {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got.stdout, wantOut)
	}
	for _, c := range w.run.checks() {
		if !slices.Equal(c.Argv[:2], []string{"ansible-playbook", "--syntax-check"}) {
			t.Fatalf("argv %v", c.Argv)
		}
	}
}

// S10
func TestAFailureWithNoStderrStatesItsExit(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.run.silentFail = "a.yml"
	wantExit(t, w.main(), ExitFailed)
	if e := w.report().Failures[0].Error; e != "ansible-playbook exited 4 with no output" {
		t.Fatalf("error %q", e)
	}
}

// S11: all checked in path order whatever finishes first, never more than four at once, and a
// failure does not stop the rest.
func TestChecksRunInParallelAndReportInPathOrder(t *testing.T) {
	w := newWorld(t, nil)
	var names []string
	for _, n := range []string{"h", "g", "f", "e", "d", "c", "b", "a"} {
		w.addPlaybook(n + ".yml")
		names = append([]string{n + ".yml"}, names...)
	}
	names = append(names, basePlaybook)
	w.addFile("c.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	w.addFile("a.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	w.run.barrier = &sync.WaitGroup{}
	w.run.barrier.Add(4)
	w.run.barrierSlots = 4
	got := w.main()
	wantExit(t, got, ExitFailed)
	if checked := checkedFiles(w.report()); !slices.Equal(checked, names) {
		t.Fatalf("results %v, want %v", checked, names)
	}
	if w.run.peak != 4 {
		t.Fatalf("peak concurrency %d, want 4", w.run.peak)
	}
	if first, second := strings.Index(got.stdout, "a.yml"), strings.Index(got.stdout, "c.yml"); first < 0 || second < first {
		t.Fatalf("failures not in path order:\n%s", got.stdout)
	}
	if len(w.run.checks()) != 9 {
		t.Fatalf("%d playbooks checked", len(w.run.checks()))
	}
}

// S12
func TestAnEngineThatDoesNotJudgeIsExitTwoNamingThePlaybook(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"start":    func(f *fakeRun) { f.startFails = "b.yml" },
		"timeout":  func(f *fakeRun) { f.timesOut = "b.yml" },
		"leftover": func(f *fakeRun) { f.leftover = "b.yml" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.addPlaybook("a.yml")
			w.addPlaybook("b.yml")
			set(w.run)
			wantStderr(t, w.main(), "b.yml")
			w.noReport()
		})
	}
}

// S14
func TestPassLineStatesThePopulation(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("playbooks/imports/a.yml")
	w.addPlaybook("playbooks/imports/b.yml")
	w.addPlaybook("playbooks/c.yml")
	got := w.main()
	wantExit(t, got, ExitOK)
	if want := "✓ ansible-syntax: 4 playbooks OK (2 under playbooks/imports/, 2 elsewhere)\n"; got.stdout != want || got.stderr != "" {
		t.Fatalf("stdout %q stderr %q", got.stdout, got.stderr)
	}
}

// S15
func TestReportShape(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.addFile("b.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	wantExit(t, w.main(), ExitFailed)
	rep := w.report()
	if rep.Type != "ansible-syntax" || rep.Status != "fail" || rep.Summary != (summary{Total: 3, Passed: 2, Failed: 1}) {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Results) != 3 || rep.Results[0] != (entry{File: "a.yml", Type: "ansible-syntax", Status: "pass"}) {
		t.Fatalf("results %+v", rep.Results)
	}
	if len(rep.Failures) != 1 || rep.Failures[0].File != "b.yml" || rep.Failures[0].Status != "fail" || !strings.Contains(rep.Failures[0].Error, "bad syntax") {
		t.Fatalf("failures %+v", rep.Failures)
	}
}

// S15
func TestPassingReportHasAnEmptyFailureList(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	wantExit(t, w.main(), ExitOK)
	if rep := w.report(); rep.Status != "pass" || rep.Summary != (summary{Total: 2, Passed: 2}) || len(rep.Failures) != 0 {
		t.Fatalf("report %+v", rep)
	}
}
