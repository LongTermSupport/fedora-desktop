package patterns

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const shebang = "#!/usr/bin/bash\n"

// oneScript is a tree with a single clean script.
func oneScript(t *testing.T) *world {
	t.Helper()
	return newWorld(t, map[string]string{"scripts/a.bash": shebang})
}

func wantExit(t *testing.T, got result, code ExitCode) {
	t.Helper()
	if got.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", got.code, code, got.stdout, got.stderr)
	}
}

func wantStderr(t *testing.T, got result, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, got.stderr)
		}
	}
}

func noReport(t *testing.T, w *world) {
	t.Helper()
	if _, err := os.Stat(w.out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a report was written on a run that could not complete: %v", err)
	}
}

// P1
func TestP1UnknownArgumentAndMissingValueAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"extra"}, {"--root"}, {"--scripts"}} {
		w := oneScript(t)
		got := w.main(args...)
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR:", "usage:")
		if len(w.run.calls) != 0 {
			t.Fatalf("%v ran a process: %v", args, w.run.calls)
		}
	}
}

// P1: an empty or flag-like --root/--scripts value is refused, not read as "not given".
func TestP1AnEmptyOrFlagLikeDirectoryValueIsRefused(t *testing.T) {
	for _, args := range [][]string{{"--root", ""}, {"--scripts", ""}, {"--root", "--scripts"}} {
		w := oneScript(t)
		got := w.main(args...)
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR: "+args[0]+" needs a directory", "usage:")
		if len(w.run.calls) != 0 {
			t.Fatalf("%v ran a process: %v", args, w.run.calls)
		}
	}
}

func TestP1HelpIsOnStdoutAndExitsZero(t *testing.T) {
	w := oneScript(t)
	got := w.main("--help")
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "usage: qa-patterns") || got.stderr != "" {
		t.Fatalf("help: %+v", got)
	}
}

func TestP1RootComesFromRootThenScriptsParentThenCwd(t *testing.T) {
	w := oneScript(t)
	other := newWorld(t, map[string]string{"scripts/b.bash": shebang})
	w.run.files = other.run.files
	wantExit(t, w.main("--root", other.root), ExitOK)
	if w.run.dirs[0] != other.root {
		t.Fatalf("--root: git ran in %q", w.run.dirs[0])
	}
	w2 := oneScript(t)
	wantExit(t, w2.main("--scripts", filepath.Join(w2.root, "scripts")), ExitOK)
	if w2.run.dirs[0] != w2.root {
		t.Fatalf("--scripts: git ran in %q, want %q", w2.run.dirs[0], w2.root)
	}
	w3 := oneScript(t)
	wantExit(t, w3.main(), ExitOK)
	if w3.run.dirs[0] != w3.root {
		t.Fatalf("cwd: git ran in %q", w3.run.dirs[0])
	}
}

// P2
func TestP2ReportPathDefaultsAndUnwritableIsExit2(t *testing.T) {
	w := oneScript(t)
	w.env["QA_JSON_OUT"] = ""
	var wrote string
	var stdout, stderr strings.Builder
	code := Main(nil, Env{
		Getenv: func(string) string { return "" }, Stdout: &stdout, Stderr: &stderr, Cwd: w.root,
		Runner: w.run, TempDir: w.tmp,
		WriteFile: func(path string, _ []byte) error { wrote = path; return nil },
	})
	if code != ExitOK || wrote != "/tmp/qa-patterns-results.json" {
		t.Fatalf("code %d wrote %q: %s", code, wrote, stderr.String())
	}
	bad := oneScript(t)
	bad.env["QA_JSON_OUT"] = filepath.Join(bad.root, "no-such-dir", "r.json")
	got := bad.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot write the report "+bad.env["QA_JSON_OUT"])
}

// P3
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestP3FailedStdoutWriteIsExit2(t *testing.T) {
	w := oneScript(t)
	var stderr strings.Builder
	code := Main(nil, Env{
		Getenv: func(k string) string { return w.env[k] }, Stdout: failingWriter{}, Stderr: &stderr, Cwd: w.root,
		Runner: w.run, TempDir: w.tmp, WriteFile: func(string, []byte) error { return nil },
	})
	if code != ExitCannotRun || !strings.Contains(stderr.String(), "ERROR: cannot write to stdout: pipe closed") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

// P4
func TestP4SemgrepMissingOrBrokenIsExit2WithTheInstallHint(t *testing.T) {
	for name, set := range map[string]func(*fakeRun){
		"not on PATH":   func(f *fakeRun) { f.noSemgrep = true },
		"version fails": func(f *fakeRun) { f.versionCode = 1 },
	} {
		w := oneScript(t)
		set(w.run)
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR: semgrep not found: run ./playbooks/imports/play-python.yml (it installs the pinned semgrep; do not install it by hand)")
		noReport(t, w)
		if w.run.called("git") {
			t.Fatalf("%s: ran git before knowing semgrep works", name)
		}
	}
}

// P5
func TestP5MissingRulesOrFixtureIsExit2NamingIt(t *testing.T) {
	for _, name := range []string{".semgrep/bash-conventions.yml", ".semgrep/bash-conventions.bash"} {
		w := newWorld(t, map[string]string{"scripts/a.bash": shebang, name: ""})
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR:", name)
		noReport(t, w)
	}
}

func TestP5AFixtureThatMarksNoRuleidLineProvesNothingAndIsExit2(t *testing.T) {
	w := newWorld(t, map[string]string{".semgrep/bash-conventions.bash": "# fixture\n# ok: bash-x\n"})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: .semgrep/bash-conventions.bash marks no ruleid line; the self-test proves nothing")
	if w.run.mirror != "" {
		t.Fatal("scanned over a fixture that proves nothing")
	}
}

func TestP5ATodoruleidLineProvesNothingAndIsExit2(t *testing.T) {
	w := newWorld(t, map[string]string{".semgrep/bash-conventions.bash": "# todoruleid: bash-x\n"})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "marks no ruleid line; the self-test proves nothing")
}

func TestP5SelfTestRunsOnRulesAndFixtureAndItsFailureIsExit2WithItsOutput(t *testing.T) {
	w := oneScript(t)
	w.run.testCode = 1
	w.run.testOut = "rule bash-x: 1 missing"
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: semgrep rule self-tests failed (see .semgrep/bash-conventions.bash)", "rule bash-x: 1 missing")
	noReport(t, w)
	argv := w.run.callWith("semgrep", "--test")
	if argv == nil || !strings.Contains(strings.Join(argv, " "), "--config "+filepath.Join(w.root, ".semgrep/bash-conventions.yml")) ||
		!strings.Contains(strings.Join(argv, " "), filepath.Join(w.root, ".semgrep/bash-conventions.bash")) {
		t.Fatalf("self-test argv: %v", argv)
	}
	if w.run.mirror != "" {
		t.Fatal("scanned after the self-test failed")
	}
}

// P6
func TestP6ScopeIsWhatGitWouldPublishMinusDeleted(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "scripts/gone.bash": shebang, "scripts/b.bash": shebang})
	w.run.files = []string{"scripts/a.bash", "scripts/gone.bash", "scripts/b.bash"}
	w.run.deleted = []string{"scripts/gone.bash"}
	if err := os.Remove(filepath.Join(w.root, "scripts/gone.bash")); err != nil {
		t.Fatal(err)
	}
	// An ignored file is on disk and not in the git listing.
	w.write("scripts/ignored.bash", shebang)
	wantExit(t, w.main(), ExitOK)
	if got := strings.Join(w.run.mirrorFiles, ","); got != "scripts/a.bash,scripts/b.bash" {
		t.Fatalf("mirror holds %s", got)
	}
	if !w.run.called("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard") {
		t.Fatalf("git calls: %v", w.run.calls)
	}
	if !w.run.called("git", "ls-files", "-z", "--deleted") {
		t.Fatalf("no --deleted listing: %v", w.run.calls)
	}
}

func TestP6GitFailingIsExit2(t *testing.T) {
	w := oneScript(t)
	w.run.gitCode = 128
	w.run.gitErr = "fatal: not a git repository"
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot list the files git would publish under "+w.root, "not a git repository")
	noReport(t, w)
}

// P7
func TestP7ExcludedTreesComeFromTheShellRowsAndMatchWholeSegments(t *testing.T) {
	w := newWorld(t, map[string]string{
		"scripts/a.bash":                shebang,
		"untracked/x.bash":              shebang,
		"deep/untracked/y.bash":         shebang,
		"untracked-not/z.bash":          shebang,
		"scripts/untrackedfoo/w.bash":   shebang,
		".venv/p.bash":                  shebang, // a python-list tree: not excluded for shell
		"scripts/qa-trees.tsv":          "shell\tuntracked\nshell\t.semgrep\n",
		".semgrep/bash-conventions.yml": "rules: []\n",
	})
	wantExit(t, w.main(), ExitOK)
	want := ".venv/p.bash,scripts/a.bash,scripts/untrackedfoo/w.bash,untracked-not/z.bash"
	if got := strings.Join(w.run.mirrorFiles, ","); got != want {
		t.Fatalf("mirror holds %s, want %s", got, want)
	}
}

func TestP7NoShellRowMalformedRowOrMissingFileIsExit2(t *testing.T) {
	for name, tsv := range map[string]string{
		"no shell row": "python\t.venv\n",
		"empty":        "\n",
		"short row":    "shell\n",
		"empty dir":    "shell\t\n",
	} {
		w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "scripts/qa-trees.tsv": tsv})
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR:", "qa-trees.tsv")
		noReport(t, w)
		_ = name
	}
	missing := newWorld(t, map[string]string{"scripts/a.bash": shebang, "scripts/qa-trees.tsv": ""})
	wantExit(t, missing.main(), ExitCannotRun)
}

// P8
func TestP8WhatIsAShellScript(t *testing.T) {
	cases := map[string]struct {
		name, text string
		want       bool
	}{
		"bash ext":              {"a.bash", "echo\n", true},
		"sh ext":                {"a.sh", "\n", true},
		"bash shebang":          {"tool", "#!/bin/bash\n", true},
		"usr bin bash":          {"tool", "#!/usr/bin/bash\nset -e\n", true},
		"env bash":              {"tool", "#!/usr/bin/env bash\n", true},
		"env sh":                {"tool", "#!/usr/bin/env sh\n", true},
		"env with option":       {"tool", "#!/usr/bin/env -S bash -e\n", true},
		"bin sh":                {"tool", "#!/bin/sh\n", true},
		"bash -c inline":        {"playbook", "#!/usr/bin/env -S bash -c 'exec x'\n", false},
		"bash path -c inline":   {"playbook", "#!/bin/bash -c 'x'\n", false},
		"bash -ec inline":       {"playbook", "#!/usr/bin/env -S bash -ec 'exec x'\n", false},
		"bash -eu is a script":  {"tool", "#!/bin/bash -eu\n", true},
		"bashful is not bash":   {"tool", "#!/usr/bin/bashful\n", false},
		"python":                {"tool", "#!/usr/bin/env python3\n", false},
		"no shebang":            {"README", "hello\n", false},
		"empty":                 {"empty", "", false},
		"template":              {"x.bash.j2", "#!/usr/bin/bash\n", false},
		"template shebang only": {"x.j2", "#!/usr/bin/bash\n", false},
		"shebang not first":     {"tool", "\n#!/bin/bash\n", false},
		"no newline":            {"tool", "#!/bin/bash", true},
	}
	for name, c := range cases {
		w := newWorld(t, map[string]string{"scripts/other.bash": shebang, "dir/" + c.name: c.text})
		wantExit(t, w.main(), ExitOK)
		rel := "dir/" + c.name
		if c.name == "other" {
			continue
		}
		in := false
		for _, f := range w.run.mirrorFiles {
			in = in || f == rel || f == rel+".bash"
		}
		if in != c.want {
			t.Errorf("%s: scanned %v, want %v (mirror %v)", name, in, c.want, w.run.mirrorFiles)
		}
	}
}

func TestP8SymlinkAndDirectoryEntriesAreNotScriptsAndSymlinksAreCounted(t *testing.T) {
	w := oneScript(t)
	w.link("scripts/alias.bash", "a.bash")
	w.run.files = append(w.run.files, "vendored-submodule")
	if err := os.Mkdir(filepath.Join(w.root, "vendored-submodule"), 0o750); err != nil {
		t.Fatal(err)
	}
	got := w.main()
	wantExit(t, got, ExitOK)
	if strings.Join(w.run.mirrorFiles, ",") != "scripts/a.bash" {
		t.Fatalf("mirror: %v", w.run.mirrorFiles)
	}
	if !strings.Contains(got.stdout, "⚠ patterns: 1 symlink(s) not scanned in their own right") {
		t.Fatalf("no symlink coverage line:\n%s", got.stdout)
	}
}

// P8, P16: a link is "not scanned in its own right" only when its target is scanned.
func TestP8LinkToAScriptOutsideTheScannedSetIsScannedUnderItsOwnNameAndNamed(t *testing.T) {
	w := oneScript(t)
	w.write("untracked/scratch/evil.bash", shebang+"cmd || true\n")
	w.link("scripts/alias.bash", "../untracked/scratch/evil.bash")
	got := w.main()
	wantExit(t, got, ExitOK)
	if strings.Join(w.run.mirrorFiles, ",") != "scripts/a.bash,scripts/alias.bash" {
		t.Fatalf("mirror: %v", w.run.mirrorFiles)
	}
	if w.run.mirrorData["scripts/alias.bash"] != shebang+"cmd || true\n" {
		t.Fatalf("the link's content: %q", w.run.mirrorData["scripts/alias.bash"])
	}
	for _, part := range []string{
		"⚠ patterns: 1 symlink(s) scanned under their own name, their targets being outside the scanned set:",
		"    scripts/alias.bash -> untracked/scratch/evil.bash",
		"✓ patterns: 2 files OK",
	} {
		if !strings.Contains(got.stdout, part) {
			t.Fatalf("stdout lacks %q:\n%s", part, got.stdout)
		}
	}
	if strings.Contains(got.stdout, "not scanned in their own right") {
		t.Fatalf("claims a target scanned that is not:\n%s", got.stdout)
	}
}

func TestP8LinkToATargetOutsideTheRootIsScannedUnderItsOwnNameToo(t *testing.T) {
	w := oneScript(t)
	outside := filepath.Join(t.TempDir(), "far.bash")
	if err := os.WriteFile(outside, []byte(shebang), 0o600); err != nil {
		t.Fatal(err)
	}
	w.link("scripts/far.bash", outside)
	got := w.main()
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "    scripts/far.bash -> "+outside) {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

func TestP8LinkToAScriptInAnExcludedTreeIsScannedUnderItsOwnName(t *testing.T) {
	w := oneScript(t)
	w.write("untracked/x.bash", shebang)
	w.link("scripts/viaexcluded", "../untracked/x.bash")
	got := w.main()
	wantExit(t, got, ExitOK)
	if !strings.Contains(got.stdout, "    scripts/viaexcluded -> untracked/x.bash") || !slices.Contains(w.run.mirrorFiles, "scripts/viaexcluded.bash") {
		t.Fatalf("mirror %v\nstdout:\n%s", w.run.mirrorFiles, got.stdout)
	}
}

func TestP8ACandidateGitListsButTheTreeLacksIsExit2NamingIt(t *testing.T) {
	w := oneScript(t)
	w.run.files = append(w.run.files, "bin/vanished")
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: cannot inspect bin/vanished")
}

func TestP8DanglingShellNamedSymlinkIsExit2ButOtherDanglingLinksAreNotScripts(t *testing.T) {
	w := oneScript(t)
	w.link("scripts/dangling", "nowhere")
	wantExit(t, w.main(), ExitOK)
	w.link("scripts/dead.bash", "nowhere")
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: the symlink scripts/dead.bash cannot be followed")
}

// P9
func TestP9NoScriptsAtAllIsExit2(t *testing.T) {
	w := newWorld(t, map[string]string{"README.md": "hi\n"})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR:", "found 0 shell scripts")
	noReport(t, w)
	if w.run.mirror != "" {
		t.Fatal("ran semgrep on nothing")
	}
}

// P10
func TestP10MirrorKeepsRelativePathsAppendsBashAndIsRemoved(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "bin/tool": shebang + "echo hi\n", "lib/x.sh": shebang})
	wantExit(t, w.main(), ExitOK)
	if got := strings.Join(w.run.mirrorFiles, ","); got != "bin/tool.bash,lib/x.sh,scripts/a.bash" {
		t.Fatalf("mirror: %s", got)
	}
	if !strings.HasPrefix(w.run.mirror, w.tmp) {
		t.Fatalf("mirror %q is not under the temp dir %q", w.run.mirror, w.tmp)
	}
	if _, err := os.Stat(w.run.mirror); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mirror left behind: %v", err)
	}
	text, err := os.ReadFile(filepath.Join(w.root, "bin/tool"))
	if err != nil || string(text) != shebang+"echo hi\n" {
		t.Fatalf("source touched: %q %v", text, err)
	}
}

func TestP10MirrorCollisionIsExit2NamingBoth(t *testing.T) {
	w := newWorld(t, map[string]string{"dir/foo": shebang, "dir/foo.bash": shebang})
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: two files claim the same scan-mirror path 'dir/foo.bash'", "dir/foo", "dir/foo.bash")
	noReport(t, w)
}

func TestP10MirrorIsRemovedWhenSemgrepFails(t *testing.T) {
	w := oneScript(t)
	w.run.scanCode = 2
	wantExit(t, w.main(), ExitCannotRun)
	if _, err := os.Stat(w.run.mirror); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mirror left behind: %v", err)
	}
}

// P11
func TestP11SemgrepRunsInTheMirrorWithTheRules(t *testing.T) {
	w := oneScript(t)
	wantExit(t, w.main(), ExitOK)
	var argv []string
	for i, c := range w.run.calls {
		if w.run.dirs[i] == w.run.mirror && c[0] == "semgrep" {
			argv = c
		}
	}
	joined := strings.Join(argv, " ")
	for _, part := range []string{"--config ../rules.yml", "--json-output", "--metrics=off", "--quiet", "--max-target-bytes=0"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("argv %q lacks %q", joined, part)
		}
	}
	if argv[len(argv)-1] != "." {
		t.Fatalf("target is not the mirror root: %v", argv)
	}
}

func TestP11RulesAreCopiedBesideTheMirrorSoRuleIdsAreBare(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, ".semgrep/bash-conventions.yml": "rules: [the-rules]\n"})
	wantExit(t, w.main(), ExitOK)
	if w.run.rulesBeside != "rules: [the-rules]\n" {
		t.Fatalf("the rules beside the mirror: %q", w.run.rulesBeside)
	}
	for _, rel := range w.run.mirrorFiles {
		if strings.Contains(rel, "rules") {
			t.Fatalf("the rules file is inside the mirror and would be scanned: %v", w.run.mirrorFiles)
		}
	}
	for _, c := range w.run.calls {
		if c[0] == "semgrep" && slices.Contains(c, "--json-output") && strings.Contains(strings.Join(c, " "), w.root) {
			t.Fatalf("the checkout path reaches semgrep's scan argv, so it reaches rule ids: %v", c)
		}
	}
}

// P10: semgrep's own ignore list would drop tests/, vendor/, build/ and more; the mirror
// overrides it with an empty one.
func TestP10MirrorHoldsAnEmptySemgrepignoreAndKeepsTestsDirectories(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "tests/x.bash": shebang})
	wantExit(t, w.main(), ExitOK)
	if w.run.ignoreFile == nil || *w.run.ignoreFile != "" {
		t.Fatalf("the mirror's .semgrepignore: %v", w.run.ignoreFile)
	}
	if got := strings.Join(w.run.mirrorFiles, ","); got != "scripts/a.bash,tests/x.bash" {
		t.Fatalf("mirror: %s", got)
	}
}

// P10: CRLF becomes LF in the copy, so a $-anchored rule matches; the real file is untouched.
func TestP10MirrorNormalisesCRLFToLFAndLeavesTheSourceAlone(t *testing.T) {
	crlf := "#!/usr/bin/bash\r\ncmd || true\r\nlone\rcr\r\n"
	w := newWorld(t, map[string]string{"scripts/a.bash": crlf})
	wantExit(t, w.main(), ExitOK)
	if got := w.run.mirrorData["scripts/a.bash"]; got != "#!/usr/bin/bash\ncmd || true\nlone\rcr\n" {
		t.Fatalf("mirror copy: %q", got)
	}
	src, err := os.ReadFile(filepath.Join(w.root, "scripts/a.bash"))
	if err != nil || string(src) != crlf {
		t.Fatalf("source touched: %q %v", src, err)
	}
}

func TestP11AnyNonZeroSemgrepStatusIsExit2WithItsStderr(t *testing.T) {
	for _, code := range []int{1, 2, 7} {
		w := oneScript(t)
		w.run.scanCode = code
		w.run.scanStderr = "boom"
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR: semgrep failed (exit", "boom")
		noReport(t, w)
	}
}

func TestP11MissingEmptyOrMalformedJSONIsExit2(t *testing.T) {
	for name, body := range map[string]string{"none": "", "garbage": "not json", "no paths": `{"results":[],"errors":[]}`, "bad error type": `{"results":[],"errors":[{"type":5}],"paths":{"scanned":["scripts/a.bash"]}}`} {
		w := oneScript(t)
		w.run.scan = func(string) string { return body }
		got := w.main()
		wantExit(t, got, ExitCannotRun)
		wantStderr(t, got, "ERROR:")
		noReport(t, w)
		_ = name
	}
}

// P12
func TestP12AFileSemgrepDidNotScanIsExit2(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "scripts/b.bash": shebang})
	w.run.scan = func(string) string { return semgrepJSON([]string{"scripts/a.bash"}, nil) }
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: semgrep did not scan 1 discovered shell script(s):", "scripts/b.bash")
	noReport(t, w)
}

func TestP12ScannedPathsMayCarryTheDotSlashPrefixAndMirrorSuffix(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "bin/tool": shebang})
	w.run.scan = func(string) string { return semgrepJSON([]string{"./scripts/a.bash", "./bin/tool.bash"}, nil) }
	wantExit(t, w.main(), ExitOK)
}

// P13
func TestP13SyntaxErrorMeansNoRuleRanAndIsExit2(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "bin/tool": shebang})
	w.run.scan = func(string) string {
		return semgrepJSON([]string{"scripts/a.bash", "bin/tool.bash"}, nil,
			`{"type":"Syntax error","path":"./bin/tool.bash"}`)
	}
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: semgrep could not parse 1 file(s), so NO rule ran on them:", "bin/tool")
	if strings.Contains(got.stderr, "tool.bash") {
		t.Fatalf("a mirror path leaked into the message:\n%s", got.stderr)
	}
	noReport(t, w)
}

// P14
func TestP14PartialParsingIsReportedWithTheUnionOfItsRangesNotGating(t *testing.T) {
	text := shebang + strings.Repeat("echo x\n", 19) // 20 lines
	w := newWorld(t, map[string]string{"scripts/a.bash": text})
	w.run.scan = func(string) string {
		return semgrepJSON([]string{"scripts/a.bash"}, nil,
			`{"type":["PartialParsing",[{"start":{"line":3},"end":{"line":6}},{"start":{"line":5},"end":{"line":8}},{"start":{"line":15},"end":{"line":16}}]],"path":"scripts/a.bash"}`)
	}
	got := w.main()
	wantExit(t, got, ExitOK)
	for _, part := range []string{
		"⚠ patterns: 1 file(s) semgrep parsed only in part (regex rules unaffected):",
		"    scripts/a.bash — 8 of 20 lines outside the parse tree (first gap from 3, last to 16)",
		"✓ patterns: 1 files OK",
	} {
		if !strings.Contains(got.stdout, part) {
			t.Fatalf("stdout lacks %q:\n%s", part, got.stdout)
		}
	}
	if strings.Index(got.stdout, "⚠") > strings.Index(got.stdout, "✓") {
		t.Fatal("the coverage lines come after the verdict")
	}
}

func TestP14AnyOtherErrorTypeIsExit2(t *testing.T) {
	w := oneScript(t)
	w.run.scan = func(string) string {
		return semgrepJSON([]string{"scripts/a.bash"}, nil, `{"type":"Timeout","path":"scripts/a.bash"}`)
	}
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR:", "Timeout", "scripts/a.bash")
	noReport(t, w)
}

func TestP14AnErrorWithNoPathNamesNoPathNotAnEmptyString(t *testing.T) {
	w := oneScript(t)
	w.run.scan = func(string) string {
		return semgrepJSON([]string{"scripts/a.bash"}, nil, `{"type":"Timeout"}`)
	}
	got := w.main()
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR:", "Timeout", "(no path)")
	if strings.Contains(got.stderr, `""`) {
		t.Fatalf("an empty quoted path:\n%s", got.stderr)
	}
}

// P15, P17, P18
func TestP15FindingsFailTheirFilesInOrderAndReachTheReport(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/b.bash": shebang, "scripts/a.bash": shebang, "scripts/c.bash": shebang, "bin/tool": shebang})
	w.run.scan = func(string) string {
		return semgrepJSON([]string{"scripts/a.bash", "scripts/b.bash", "scripts/c.bash", "bin/tool.bash"}, []hit{
			{"scripts/b.bash", "bash-z", "second rule", 9},
			{"./bin/tool.bash", "bash-a", "the  message\nover   lines\n", 4},
			{"scripts/b.bash", "bash-a", "first rule", 9},
			{"scripts/b.bash", "bash-q", "earlier line", 2},
		})
	}
	got := w.main()
	wantExit(t, got, ExitFailed)
	want := "✗ patterns: 2/4 files failed → " + w.out + "\n" +
		"  ✗ bin/tool\n    bash-a:4 the message over lines\n" +
		"  ✗ scripts/b.bash\n    bash-q:2 earlier line; bash-a:9 first rule; bash-z:9 second rule\n"
	if !strings.HasSuffix(got.stdout, want) {
		t.Fatalf("stdout:\n%s\nwant suffix:\n%s", got.stdout, want)
	}
	rep := w.report()
	sum := obj(t, rep["summary"])
	if rep["type"] != "patterns" || rep["status"] != "fail" || sum["total"] != 4.0 || sum["failed"] != 2.0 || sum["passed"] != 2.0 {
		t.Fatalf("report: %v", rep)
	}
	fails := list(t, rep["failures"])
	first := obj(t, fails[0])
	if len(fails) != 2 || first["file"] != "bin/tool" || first["type"] != "patterns" || first["status"] != "fail" || first["error"] != "bash-a:4 the message over lines" {
		t.Fatalf("failures: %v", fails)
	}
	scanned := list(t, rep["scanned"])
	if len(scanned) != 4 || scanned[0] != "bin/tool" {
		t.Fatalf("scanned lists mirror names or is unordered: %v", scanned)
	}
}

func TestP17CleanRunPrintsTheOKLineAndWritesAPassReport(t *testing.T) {
	w := newWorld(t, map[string]string{"scripts/a.bash": shebang, "scripts/b.bash": shebang})
	got := w.main()
	wantExit(t, got, ExitOK)
	if got.stdout != "✓ patterns: 2 files OK\n" || got.stderr != "" {
		t.Fatalf("output: %q / %q", got.stdout, got.stderr)
	}
	rep := w.report()
	sum := obj(t, rep["summary"])
	if rep["status"] != "pass" || sum["total"] != 2.0 || sum["passed"] != 2.0 || sum["failed"] != 0.0 {
		t.Fatalf("report: %v", rep)
	}
	if fails, ok := rep["failures"].([]any); !ok || len(fails) != 0 {
		t.Fatalf("failures must be an empty list, not null: %v", rep["failures"])
	}
	data, err := os.ReadFile(w.out)
	if err != nil || !strings.Contains(string(data), "\n  \"type\": \"patterns\"") {
		t.Fatalf("report is not two-space JSON: %q", data)
	}
}
