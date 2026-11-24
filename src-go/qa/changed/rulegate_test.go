package changed

import (
	"context"
	"encoding/json"
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

// ruleReads is whether some rule of the rule file reads a path, by the same matcher qa-changed
// plans with (semgrepreport.ParseScope). It is not an independent oracle: that the matcher agrees
// with semgrep is TestTheRuleGateReaderAgreesWithRealSemgrep's to prove.
func ruleReads(t *testing.T, root, ruleFile string) func(string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ruleFile))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := semgrepreport.ParseScope(ruleFile, data)
	if err != nil {
		t.Fatal(err)
	}
	return scope.Reads
}

// rule-gate-reads-nothing (issue #403): no file of the tree, changed alone, plans a rule-file
// check whose rule file reads none of it, as the planner's own matcher judges. For the eight
// ghscopes gates this holds by construction in a local run (dropUnreadRuleGates uses the same
// matcher), so this sweep cannot go red for them; what it can catch is a check not covered by the
// drop: the per-file bash-conventions `semgrep` check, or a gate added to gateCheck without a
// spec in ruleGateSpecs. Whether the matcher agrees with semgrep itself, which is what keeps a
// dropped gate safe, is proven against the real engine by
// TestTheRuleGateReaderAgreesWithRealSemgrep. A file that is part of the gate itself runs it
// whole and is not judged; a file scripts/qa-trees.tsv keeps out of every gate is never handed to
// semgrep at all.
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

// seedRuleGates writes into a test repository what planning a semgrep gate reads there: the real
// rule file of each gate and the real trees table (a world may then override either).
func seedRuleGates(t *testing.T, root string) {
	t.Helper()
	for _, ruleFile := range ruleGateRules() {
		body, err := os.ReadFile(filepath.Join("../../..", ruleFile))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, ruleFile, string(body))
	}
	writeFile(t, root, "scripts/qa-trees.tsv", readReal(t, "qa-trees.tsv"))
}

// planOf is the plan lines of a --plan run over the named files, in a run world that holds the
// real rule files of the semgrep gates.
func planOf(t *testing.T, files map[string]string) string {
	t.Helper()
	return planIn(t, newRunWorld(t), files)
}

// planIn is planOf in a given run world (one whose environment a test has set).
func planIn(t *testing.T, w *runWorld, files map[string]string) string {
	t.Helper()
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

// CI's diff run plans exactly what it did before issue #403: the drop is a local run's only, and
// CI runs these gates whole (semgrepGate), so a plan-only change still plans both gates there.
func TestCIStillPlansTheRuleGatesForAPlanOnlyChange(t *testing.T) {
	w := newRunWorld(t)
	w.env["CI"] = "true"
	out := planIn(t, w, map[string]string{
		"CLAUDE/Plan/00001-x/PLAN.md": "# plan\n",
		"CLAUDE/Plan/README.md":       "# plans\n",
	})
	for _, id := range []string{"place-issued-token", "gh-poll"} {
		if !slices.Contains(lines(out), "check "+id) {
			t.Errorf("CI does not plan %s for a plan-only change:\n%s", id, out)
		}
		if strings.Contains(out, id+": not needed") {
			t.Errorf("CI drops %s:\n%s", id, out)
		}
	}
}

// Every semgrep gate qa-changed runs has the engine spec its drop reads, and no spec names a check
// that is not a gate: a gate missing here would never be dropped, a stray spec never consulted.
func TestEveryRuleGateHasItsSpec(t *testing.T) {
	gates := slices.Sorted(maps.Values(gateCheck))
	specs := slices.Sorted(maps.Keys(ruleGateSpecs))
	if !slices.Equal(gates, specs) {
		t.Fatalf("gateCheck checks %v, ruleGateSpecs %v", gates, specs)
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
	out := planOf(t, map[string]string{".semgrep/gh-poll.fixture": "# changed\n"})
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

// samplesPerPattern bounds how many real files, each from its own directory, stand for one glob.
const samplesPerPattern = 6

// rulePatterns is every distinct paths.include and paths.exclude glob of a rule file.
func rulePatterns(t *testing.T, root, ruleFile string) []string {
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
	var out []string
	for _, r := range doc.Rules {
		out = append(out, r.Paths.Include...)
		out = append(out, r.Paths.Exclude...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// sampleFiles is, for each glob, up to samplesPerPattern of the files it matches, each from its
// own directory, plus up to samplesPerPattern files no glob matches: a representative set of real
// tree paths for every pattern the rule file holds.
func sampleFiles(t *testing.T, files, patterns []string) []string {
	t.Helper()
	var out []string
	pick := func(matches func(string) bool) {
		dirs := map[string]bool{}
		for _, rel := range files {
			if len(dirs) == samplesPerPattern {
				return
			}
			if dir := filepath.Dir(rel); matches(rel) && !dirs[dir] {
				dirs[dir] = true
				out = append(out, rel)
			}
		}
	}
	all, err := semgrepreport.CompileExcludes(patterns)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range patterns {
		one, err := semgrepreport.CompileExcludes([]string{p})
		if err != nil {
			t.Fatal(err)
		}
		before := len(out)
		pick(one.Matches)
		if len(out) == before {
			t.Logf("%s matches no published file; no real file stands for it", p)
		}
	}
	pick(func(rel string) bool { return !all.Matches(rel) })
	slices.Sort(out)
	return slices.Compact(out)
}

// semgrepScanned is the set of files real semgrep scans when given the rule file and the files by
// name from the repository root, as the gate hands them (ghscopes scan).
func semgrepScanned(t *testing.T, root, ruleFile string, files []string) map[string]bool {
	t.Helper()
	argv := append([]string{"semgrep", "--config", filepath.Join(root, ruleFile), "--json", "--quiet", "--metrics=off", semgrepreport.TimeoutFlag, "--max-target-bytes=0"}, files...)
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: argv, Dir: root})
	if err != nil || res.Code >= 2 || res.TimedOut || res.Leftover {
		t.Fatalf("semgrep over %s: %v exit %d\n%s", ruleFile, err, res.Code, res.Stderr)
	}
	var rep struct {
		Paths *struct {
			Scanned []string `json:"scanned"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &rep); err != nil || rep.Paths == nil {
		t.Fatalf("semgrep over %s: no paths in its report: %v", ruleFile, err)
	}
	scanned := map[string]bool{}
	for _, p := range rep.Paths.Scanned {
		scanned[strings.TrimPrefix(p, "./")] = true
	}
	return scanned
}

// readButJudgedUnread is every file semgrep scanned that reads says is not read: a file whose
// change alone would drop a gate that should have run.
func readButJudgedUnread(files []string, scanned map[string]bool, reads func(string) (bool, error)) ([]string, error) {
	var out []string
	for _, rel := range files {
		r, err := reads(rel)
		if err != nil {
			return nil, err
		}
		if scanned[rel] && !r {
			out = append(out, rel)
		}
	}
	return out, nil
}

// The safety of the drop (dropUnreadRuleGates) is that Reader.Reads never says "not read" of a
// file semgrep would scan. For each gate's real rule file, real semgrep is handed a sample of real
// tree paths standing for every include and exclude glob of the file, and every file it scans
// must be one the reader says it reads. A machine without semgrep fails here; CI installs it
// (.github/workflows/qa.yml, "Install QA tooling"). The control (a reader that reads nothing)
// shows the comparison can go red.
func TestTheRuleGateReaderAgreesWithRealSemgrep(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files := ourRegularFiles(t, root)
	for _, id := range slices.Sorted(maps.Keys(ruleGateSpecs)) {
		reader, err := ruleGateSpecs[id].Reader(root)
		if err != nil {
			t.Fatal(err)
		}
		ruleFile := reader.RuleFile()
		sample := sampleFiles(t, files, rulePatterns(t, root, ruleFile))
		scanned := semgrepScanned(t, root, ruleFile, sample)
		wrong, err := readButJudgedUnread(sample, scanned, reader.Reads)
		if err != nil {
			t.Fatal(err)
		}
		if len(wrong) > 0 {
			t.Errorf("%s: semgrep scans %d file(s) the reader says %s does not read, so a change of them alone drops a gate that should run: %v", id, len(wrong), ruleFile, wrong)
		}
		control, err := readButJudgedUnread(sample, scanned, func(string) (bool, error) { return false, nil })
		if err != nil || len(control) == 0 {
			t.Errorf("%s: a reader that reads nothing is not caught (semgrep scanned none of %d sampled files): %v", id, len(sample), err)
		}
		if len(scanned) == len(sample) {
			t.Errorf("%s: semgrep scanned every sampled file, so no exclude of %s was exercised", id, ruleFile)
		}
	}
}
