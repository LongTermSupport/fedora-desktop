package changed

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// ruleGateRules is every check that hands changed files to one semgrep rule file and refuses a
// run in which semgrep read none of them, by the rule file it runs: each ghscopes engine gate
// (gateCheck; check X runs .semgrep/X.yml) and the per-file bash-conventions check. The sttrules
// engine's gates are not here: they drop a file every rule of its language excludes themselves
// (S10a).
func ruleGateRules() map[string]string {
	rules := map[string]string{"semgrep": ".semgrep/bash-conventions.yml"}
	for _, id := range gateCheck {
		rules[id] = ".semgrep/" + id + ".yml"
	}
	return rules
}

// ruleReads is a test-side reading of a rule file, independent of the code it judges: whether
// some rule of the file reads a path, by that rule's own paths.include and paths.exclude.
func ruleReads(t *testing.T, root, ruleFile string) func(string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ruleFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rules []struct {
			Paths struct {
				Include []string `yaml:"include"`
				Exclude []string `yaml:"exclude"`
			} `yaml:"paths"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", ruleFile, err)
	}
	if len(doc.Rules) == 0 {
		t.Fatalf("%s holds no rule", ruleFile)
	}
	type scope struct{ include, exclude semgrepreport.Excludes }
	scopes := make([]scope, 0, len(doc.Rules))
	for _, r := range doc.Rules {
		include, err := semgrepreport.CompileExcludes(r.Paths.Include)
		if err != nil {
			t.Fatal(err)
		}
		exclude, err := semgrepreport.CompileExcludes(r.Paths.Exclude)
		if err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope{include, exclude})
	}
	return func(rel string) bool {
		return slices.ContainsFunc(scopes, func(s scope) bool {
			return (len(s.include) == 0 || s.include.Matches(rel)) && !s.exclude.Matches(rel)
		})
	}
}

func TestRuleReadsKeepsAFileOneRuleOfSeveralStillReads(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".semgrep/two.yml", `rules:
  - id: a
    paths:
      exclude: ['docs/**']
  - id: b
    paths:
      exclude: ['docs/**', 'notes/**']
  - id: c
    paths:
      include: ['notes/**', 'docs/**']
      exclude: ['notes/**']
`)
	reads := ruleReads(t, root, ".semgrep/two.yml")
	for rel, want := range map[string]bool{"docs/a.md": true, "notes/a.md": true, "src/a.go": true} {
		if got := reads(rel); got != want {
			t.Errorf("reads(%s) = %v, want %v", rel, got, want)
		}
	}
	writeFile(t, root, ".semgrep/one.yml", "rules:\n  - id: a\n    paths:\n      exclude: ['docs/**']\n  - id: b\n    paths:\n      include: ['src/**']\n")
	reads = ruleReads(t, root, ".semgrep/one.yml")
	for rel, want := range map[string]bool{"docs/a.md": false, "src/a.go": true, "x.md": true} {
		if got := reads(rel); got != want {
			t.Errorf("one rule file: reads(%s) = %v, want %v", rel, got, want)
		}
	}
}

// rule-gate-reads-nothing (issue #403): no file of the tree, changed alone, plans a semgrep rule
// gate whose rule file reads none of it. Such a plan hands semgrep only files its rules exclude,
// semgrep scans nothing, and the gate's fail-closed guard ("semgrep scanned no file") makes the
// change one that can never pass, as a plan-only commit could not. A file that is part of the
// gate itself runs it whole and is not judged; a file scripts/qa-trees.tsv keeps out of every
// gate is never handed to semgrep at all. Every routing row is swept through the files it
// routes today, so a row added later is held as soon as it routes a file of the tree.
func TestNoRealFilePlansARuleGateWhoseRuleReadsNoneOfIt(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	rules := ruleGateRules()
	reads := map[string]func(string) bool{}
	for id, ruleFile := range rules {
		if !isCheckID(id) {
			t.Fatalf("%s is no check", id)
		}
		reads[id] = ruleReads(t, root, ruleFile)
	}
	s := &session{
		env:     Env{Getenv: noEnv, Umask: keepUmask, Runner: proc.OS{}, Stderr: &strings.Builder{}, Stdout: &strings.Builder{}},
		root:    root,
		scripts: filepath.Join(root, "scripts"),
		opts:    options{base: "HEAD"},
	}
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	files := ourRegularFiles(t, root)
	if len(files) < 1000 {
		t.Fatalf("found only %d published files; the tree walk matched nothing", len(files))
	}
	found := map[string][]string{}
	for _, rel := range files {
		s.plan = &plan{checks: map[string]*checkPlan{}, covered: map[string]bool{}}
		if err := s.classify(context.Background(), collected{existing: []string{rel}}); err != nil {
			t.Fatal(err)
		}
		for id, read := range reads {
			if s.plan.has(id) && !gateSource("qa-"+id+".bash", rel) && !read(rel) {
				found[id] = append(found[id], rel)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(found)) {
		got := found[id]
		t.Errorf("%d file(s) plan the %s check, whose rule %s reads none of them, so a change of only such files fails closed; first: %v",
			len(got), id, rules[id], got[:min(len(got), 5)])
	}
}

// planOf is the plan lines of a --plan run over the named files, in a run world that holds the
// real rule files of the semgrep gates.
func planOf(t *testing.T, files map[string]string) string {
	t.Helper()
	w := newRunWorld(t)
	for _, ruleFile := range ruleGateRules() {
		body, err := os.ReadFile(filepath.Join("../../..", ruleFile))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, w.root, ruleFile, string(body))
	}
	args := []string{"--plan", "--files"}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		writeFile(t, w.root, rel, files[rel])
		args = append(args, rel)
	}
	if code := w.main(args...); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	return w.out.String()
}

// The reproduction of issue #403: a change of plan prose alone plans neither gate, and says why.
func TestAPlanOnlyChangePlansNoRuleGateWhoseRuleExcludesIt(t *testing.T) {
	out := planOf(t, map[string]string{
		"CLAUDE/Plan/00001-x/PLAN.md":                     "# plan\n",
		"CLAUDE/Plan/00001-x/JOURNAL/00001-Journal-01.md": "# journal\n",
		"CLAUDE/Plan/README.md":                           "# plans\n",
	})
	for _, id := range []string{"place-issued-token", "gh-poll"} {
		if slices.Contains(lines(out), "check "+id) {
			t.Errorf("a plan-only change plans %s:\n%s", id, out)
		}
		if !strings.Contains(out, "note: "+id+": not needed: nothing for this rule to scan") {
			t.Errorf("no note says why %s is not run:\n%s", id, out)
		}
	}
}

// A file the trees table keeps out of every gate is not one the rule reads either.
func TestARuleExcludedFileBesideANotOursFilePlansNoRuleGate(t *testing.T) {
	out := planOf(t, map[string]string{
		"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n",
		"untracked/notes.md":          "# notes\n",
	})
	if slices.Contains(lines(out), "check gh-poll") {
		t.Errorf("gh-poll is planned over files it reads none of:\n%s", out)
	}
}

// A file the rule does read, beside the excluded ones, still runs the gate.
func TestARuleReadFileBesideExcludedOnesStillPlansTheRuleGate(t *testing.T) {
	out := planOf(t, map[string]string{
		"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n",
		"docs/guide.md":               "# guide\n",
	})
	for _, id := range []string{"place-issued-token", "gh-poll"} {
		if !slices.Contains(lines(out), "check "+id) {
			t.Errorf("%s is not planned for a file its rule reads:\n%s", id, out)
		}
	}
}

// A change to the gate itself runs it, even when the rule excludes the changed file (its own rule
// file and fixture always are).
func TestAChangeToTheGatesOwnRuleStillPlansIt(t *testing.T) {
	out := planOf(t, map[string]string{".semgrep/gh-poll.fixture": "gh run watch\n"})
	if !slices.Contains(lines(out), "check gh-poll") {
		t.Errorf("a change to the gh-poll fixture does not run gh-poll:\n%s", out)
	}
}

// ourRegularFiles is every regular file git would publish that scripts/qa-trees.tsv does not keep
// out of every gate.
func ourRegularFiles(t *testing.T, root string) []string {
	t.Helper()
	repo := os.DirFS(root)
	table, err := qatrees.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, rel := range publishedUnder(t, root, ".") {
		if info, err := os.Lstat(filepath.Join(root, rel)); err != nil || !info.Mode().IsRegular() {
			continue
		}
		notOurs, err := table.ExcludesFile(repo, rel)
		if err != nil {
			t.Fatal(err)
		}
		if !notOurs {
			out = append(out, rel)
		}
	}
	return out
}
