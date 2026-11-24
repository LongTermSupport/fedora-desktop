package ghscopes

import (
	"path/filepath"
	"slices"
	"testing"
)

// Named files (after --): the gate scans exactly those, never asks git for the whole tree, and
// still proves the rule against its fixture first.
func TestNamedFilesAreScannedInsteadOfTheWholeTree(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n"})
	got := w.main("--", "b.txt")
	wantExit(t, got, ExitOK)
	if w.run.called("git") {
		t.Fatalf("a named run must not list the tree: %v", w.run.calls)
	}
	if files := handed(w.run.scanArgv); !slices.Equal(files, []string{"b.txt"}) {
		t.Fatalf("handed %v, want [b.txt]", files)
	}
	if !w.run.selfTested(filepath.Join(w.root, ruleFile), ".fixture") {
		t.Fatalf("the rule was not proven first: %v", w.run.calls)
	}
	wantIn(t, "stdout", got.stdout, "passed: 1 failed: 0")
}

// A named file that is gone, a symlink, or one the trees table keeps out of every gate is not
// handed over; with nothing left the run passes without scanning, saying so.
func TestNamedFilesOutOfScopeAreDroppedAndNoneLeftPasses(t *testing.T) {
	w := newWorld(t, map[string]string{".claude/ccy/x.bash": "x\n"})
	w.link("alias.txt", "a.txt")
	got := w.main("--", "gone.txt", "alias.txt", ".claude/ccy/x.bash")
	wantExit(t, got, ExitOK)
	if w.run.scanArgv != nil {
		t.Fatalf("nothing was in scope, yet semgrep scanned: %v", w.run.scanArgv)
	}
	wantIn(t, "stdout", got.stdout, "passed: 0 failed: 0", "none of the 3 named file(s)")
}

// A named file every rule of the rule file excludes is not handed over either (issue #403): a run
// named only such files (a plan-only change) has nothing for the rule to scan, and passes saying
// so, rather than handing semgrep files it drops and failing closed.
func TestNamedFilesTheRuleExcludesAreDroppedAndNoneLeftPasses(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n"})
	got := w.main("--", "CLAUDE/Plan/00001-x/PLAN.md")
	wantExit(t, got, ExitOK)
	if w.run.scanArgv != nil {
		t.Fatalf("the rule excludes the only named file, yet semgrep scanned: %v", w.run.scanArgv)
	}
	wantIn(t, "stdout", got.stdout, "passed: 0 failed: 0", "none of the 1 named file(s)")
}

// Beside a file the rule reads, an excluded one is left out and the read one is scanned.
func TestANamedFileTheRuleReadsIsScannedBesideAnExcludedOne(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n", "b.txt": "x\n"})
	got := w.main("--", "CLAUDE/Plan/00001-x/PLAN.md", "b.txt")
	wantExit(t, got, ExitOK)
	if files := handed(w.run.scanArgv); !slices.Equal(files, []string{"b.txt"}) {
		t.Fatalf("handed %v, want [b.txt]", files)
	}
}

// The fail-closed guard still holds for a named file the rule reads: semgrep scanning none of
// the files it was handed, for any reason it does not give, is a run that could not judge.
func TestANamedRunWhoseReadFileSemgrepDidNotScanIsExitTwo(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n", "b.txt": "x\n"})
	w.run.scan = func([]string) string { return report(nil, nil, nil) }
	got := w.main("--", "CLAUDE/Plan/00001-x/PLAN.md", "b.txt")
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR:")
}

// Reader is the same judgement for a caller planning the gate (qa-changed): the trees table, then
// the rule file's own paths.
func TestTheReaderAnswersWhichChangedFilesTheGateReads(t *testing.T) {
	w := newWorld(t, map[string]string{"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n", ".claude/ccy/x.bash": "x\n"})
	r, err := GHScopes.Reader(w.root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{"a.txt": true, "CLAUDE/Plan/00001-x/PLAN.md": false, ".claude/ccy/x.bash": false} {
		got, err := r.Reads(rel)
		if err != nil || got != want {
			t.Errorf("Reads(%s) = %v, %v; want %v", rel, got, err, want)
		}
	}
	if r.RuleFile() != ruleFile {
		t.Errorf("RuleFile() = %s", r.RuleFile())
	}
}

// A rule file or trees table the reader cannot read is an error, never "reads nothing".
func TestTheReaderRefusesAMissingRuleOrTable(t *testing.T) {
	for _, gone := range []string{ruleFile, "scripts/qa-trees.tsv"} {
		w := newWorld(t, map[string]string{gone: ""})
		if _, err := GHScopes.Reader(w.root); err == nil {
			t.Errorf("no %s, yet the reader loaded", gone)
		}
	}
	w := newWorld(t, map[string]string{ruleFile: "rules: []\n"})
	if _, err := GHScopes.Reader(w.root); err == nil {
		t.Error("a rule file with no rule loaded")
	}
}

// A finding in a named file fails the run, as in the whole-tree run.
func TestAFindingInANamedFileFails(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n"})
	w.run.scan = func(files []string) string { return report(files, []hit{{"b.txt", 1}}, nil) }
	got := w.main("--", "b.txt")
	wantExit(t, got, ExitFailed)
	wantIn(t, "stdout", got.stdout, "b.txt:1  gh-scope-outside-ssot", "passed: 0 failed: 1")
}

// A named file semgrep skips with a reason (a language the rule has no parser for) is not a lost
// file; a named run where every file was skipped so has nothing to judge, and passes.
func TestNamedFilesAllSkippedWithAReasonPass(t *testing.T) {
	w := newWorld(t, map[string]string{"b.txt": "x\n"})
	w.run.scan = func(files []string) string { return report(nil, nil, files) }
	got := w.main("--", "b.txt")
	wantExit(t, got, ExitOK)
	wantIn(t, "stdout", got.stdout, "1 file(s) skipped by semgrep", "passed: 0 failed: 0")
}

// A named path must be repository-relative and stay inside the root.
func TestANamedPathOutsideTheRootIsRefused(t *testing.T) {
	for _, bad := range []string{"../x", "/etc/passwd", ""} {
		got := newWorld(t, nil).main("--", bad)
		wantExit(t, got, ExitCannotRun)
		wantIn(t, "stderr", got.stderr, "ERROR:", "repository-relative")
	}
}
