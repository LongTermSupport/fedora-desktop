package changed

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// treesFile is the trees table as a changed file, relative to the root.
const treesFile = "scripts/qa-trees.tsv"

// gitShowTrees is the call that reads the trees table at the base of a classify world.
const gitShowTrees = "git show HEAD:" + treesFile

// treesPlan classifies a change of the trees table from base to work, the base served as git
// would show it (or failing, when showErr is set), and returns the session after it.
func treesPlan(t *testing.T, base, work string, show proc.Result) *session {
	t.Helper()
	w := newWorld(t)
	writeFile(t, w.root, treesFile, work)
	show.Stdout = base
	w.runner.replies[gitShowTrees] = show
	s := w.session()
	if err := s.classify(context.Background(), collected{existing: []string{treesFile}}); err != nil {
		t.Fatal(err)
	}
	s.settleUncovered()
	return s
}

// wholeTreesChecks is what a change to the table's rows plans: every check a row naming the table
// decides, the whole Go gate among them. Each must be in a whole plan and none in a comment-only one.
var wholeTreesChecks = []string{"patterns", "bash", "python", "js", "ansible-syntax-gate", "docs", "ready-wait", "stt-rules", "walk-open"}

func requireWholeTreesPlan(t *testing.T, s *session) {
	t.Helper()
	p := s.plan
	for _, id := range wholeTreesChecks {
		if !p.has(id) {
			t.Errorf("a change to the table's rows does not plan %s: %v", id, keys(p))
		}
	}
	if !slices.Contains(p.goFull, treesFile) {
		t.Errorf("a change to the table's rows does not run the whole Go gate: goFull %v", p.goFull)
	}
	if slices.Contains(p.commentsOnly, treesFile) {
		t.Errorf("the table is recorded as a comment-only change: %v", p.commentsOnly)
	}
}

// A change to the table's comments and blank lines alone changes nothing any gate reads, so it
// plans only the table's own drift tests (issue #413), not every gate whole.
func TestACommentOnlyChangeToTheTreesTablePlansNoWholeGate(t *testing.T) {
	base := readReal(t, "qa-trees.tsv")
	work := "# a new comment line\n\n" + strings.Replace(base, "\n#", "\n#  reworded", 1) + "\n\n# trailing comment\n"
	s := treesPlan(t, base, work, proc.Result{})
	p := s.plan
	for _, id := range wholeTreesChecks {
		if p.has(id) {
			t.Errorf("a comment-only change plans %s: %v", id, keys(p))
		}
	}
	if got := keys(p); !slices.Equal(got, []string{"gitleaks", "go"}) {
		t.Errorf("checks %v, want only the drift tests' go check and gitleaks", got)
	}
	if len(p.goFull) != 0 {
		t.Errorf("the whole Go gate is planned: goFull %v", p.goFull)
	}
	if !slices.Equal(slices.Sorted(slices.Values(p.goPackages)), []string{"qa/bashlimits", "qa/ghscopes"}) {
		t.Errorf("go packages %v, want the two qa-trees-drift packages", p.goPackages)
	}
	if s.goFullRun(p.items("go")) {
		t.Error("the go check would run the whole gate, not diff mode with the drift packages")
	}
	if !p.covered[treesFile] || len(p.uncovered) != 0 {
		t.Errorf("covered %v uncovered %v", p.covered, p.uncovered)
	}
	if !slices.Equal(p.commentsOnly, []string{treesFile}) {
		t.Errorf("commentsOnly %v", p.commentsOnly)
	}
	if s.gateChanged("qa-gh-scopes.bash") {
		t.Error("a comment-only change to the table counts as a change to the semgrep gates, which would run them whole")
	}
	if !slices.ContainsFunc(p.notes, func(n string) bool {
		return strings.HasPrefix(n, treesFile+": only its comments or blank lines changed")
	}) {
		t.Errorf("no note says why the table runs so little: %v", p.notes)
	}
}

// A change to any row, an added row or a removed one, still plans every check the table's rows
// decide, the whole Go gate among them, as before.
func TestARowChangeToTheTreesTablePlansEveryGateWhole(t *testing.T) {
	base := readReal(t, "qa-trees.tsv")
	if !strings.Contains(base, "shell\t.git\n") {
		t.Fatal("the real table has no 'shell<TAB>.git' row for the fixtures to change")
	}
	for name, work := range map[string]string{
		"changed row": strings.Replace(base, "shell\t.git\n", "shell\t.gitx\n", 1),
		"added row":   base + "shell\tsome/new/tree\n",
		"removed row": strings.Replace(base, "shell\t.git\n", "", 1),
		"moved row":   strings.Replace(base, "shell\t.git\n", "", 1) + "shell\t.git\n",
	} {
		t.Run(name, func(t *testing.T) {
			requireWholeTreesPlan(t, treesPlan(t, base, work, proc.Result{}))
		})
	}
}

// A base that cannot be read or parsed is a change to the rows: the plan never runs fewer
// checks because qa-changed could not compare, and a note says why.
func TestATreesTableBaseThatCannotBeReadOrParsedPlansEveryGateWhole(t *testing.T) {
	table := readReal(t, "qa-trees.tsv")
	work := table + "# a comment\n"
	for name, c := range map[string]struct {
		base string
		show proc.Result
		note string
	}{
		"git cannot show it": {"", proc.Result{Code: 128, Stderr: "fatal: path 'scripts/qa-trees.tsv' does not exist in 'HEAD'"}, "cannot read it at HEAD"},
		"malformed base":     {"shell\n" + table, proc.Result{}, "cannot compare its rows"},
		"base with no rows":  {"# only comments\n", proc.Result{}, "cannot compare its rows"},
	} {
		t.Run(name, func(t *testing.T) {
			s := treesPlan(t, c.base, work, c.show)
			requireWholeTreesPlan(t, s)
			if !slices.ContainsFunc(s.plan.notes, func(n string) bool { return strings.Contains(n, c.note) }) {
				t.Errorf("no note says %q: %v", c.note, s.plan.notes)
			}
		})
	}
}

// A table qatrees.Parse refuses (here one with no python row, which qa-changed's own loader
// accepts) is never a comment-only change, even when its rows are those at the base.
func TestATreesTableTheParserRefusesPlansEveryGateWhole(t *testing.T) {
	var shellOnly strings.Builder
	for line := range strings.Lines(readReal(t, "qa-trees.tsv")) {
		if !strings.HasPrefix(line, "python\t") {
			shellOnly.WriteString(line)
		}
	}
	s := treesPlan(t, shellOnly.String(), shellOnly.String()+"# a comment\n", proc.Result{})
	requireWholeTreesPlan(t, s)
	if !slices.ContainsFunc(s.plan.notes, func(n string) bool { return strings.Contains(n, "cannot compare its rows") }) {
		t.Errorf("no note says why: %v", s.plan.notes)
	}
}

// An explicit file list has no base to compare with, so the table gets every check its rows decide.
func TestAnExplicitListNamingTheTreesTablePlansEveryGateWhole(t *testing.T) {
	w := newWorld(t)
	s := w.session()
	s.opts = options{explicit: true}
	if err := s.classify(context.Background(), collected{existing: []string{treesFile}}); err != nil {
		t.Fatal(err)
	}
	requireWholeTreesPlan(t, s)
	if slices.ContainsFunc(w.runner.calls, func(c proc.Cmd) bool { return c.Argv[0] == "git" }) {
		t.Errorf("an explicit list asked git for a base: %v", w.runner.argv())
	}
}

// End to end over a real git base: a comment-only edit of the committed table runs the go check in
// diff mode with the drift packages, and leaves a semgrep gate another changed file asks for
// narrowed to the changed files, since the table's rows, which the gate reads, did not change.
func TestACommentOnlyTreesTableChangeRunsTheDriftTestsAndKeepsTheGatesNarrow(t *testing.T) {
	w := newRunWorld(t)
	path := filepath.Join(w.root, treesFile)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.root, treesFile, string(body)+"# a comment only\n")
	w.changeKept()
	if code := w.main(); code != ExitOK {
		t.Fatalf("exit %d, stderr %q\n%s", code, w.errOut.String(), w.out.String())
	}
	goScript := filepath.Join(w.scripts, goGateScript)
	if got := argvOf(w.runner.calls, goScript); !slices.Contains(got, "--changed") || !slices.Contains(got, "qa/bashlimits,qa/ghscopes") {
		t.Errorf("the go check ran as %v, want diff mode with the drift packages", got)
	}
	for _, id := range []string{"python", "js", "patterns", "docs"} {
		if strings.Contains(w.out.String(), "check "+id+"\n") {
			t.Errorf("plan has check %s:\n%s", id, w.out.String())
		}
	}
	argvLeak := filepath.Join(w.scripts, "qa-argv-leak.bash")
	if got := argvOf(w.runner.calls, argvLeak); !slices.Equal(got, []string{toolBash, argvLeak, "--", "kept.bash", treesFile}) {
		t.Errorf("argv-leak ran as %v, want it narrowed to the changed files", got)
	}
}
