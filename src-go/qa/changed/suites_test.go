package changed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// fakeGates is a gate table: a suite run twice under two names, a gate, a per-file gate, a
// suite whose name has dots and dashes, and a module gate that has no script.
const fakeGates = `# a comment
gate	alpha	scripts/test-alpha.bash	alpha failed	count
gate	one	scripts/qa-gate-one.bash	one failed	count
gate	alpha-again	scripts/test-alpha.bash	alpha failed	count
gate	bash-gate	scripts/qa-bash.bash	bash failed	count
gate	dots	scripts/test-with.dots_and-dash2.bash	dots failed	count
gate	module	python3 -m helpers.x	module failed	count
`

func populationWorld(t *testing.T) string {
	t.Helper()
	scripts := filepath.Join(t.TempDir(), "scripts")
	writeFile(t, scripts, "qa-gates.tsv", fakeGates)
	for _, name := range []string{"test-alpha.bash", "qa-gate-one.bash", "qa-bash.bash", "test-with.dots_and-dash2.bash"} {
		writeFile(t, scripts, name, "#!/usr/bin/bash\n")
	}
	return scripts
}

func TestPopulationIsTheScriptsOfTheGateTableMinusThePerFileGates(t *testing.T) {
	scripts := populationWorld(t)
	got, err := loadPopulation(scripts, []string{"qa-bash"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"test-alpha.bash", "qa-gate-one.bash", "test-with.dots_and-dash2.bash"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPopulationRefusesATableItCannotUse(t *testing.T) {
	scripts := filepath.Join(t.TempDir(), "scripts")
	writeFile(t, scripts, "qa-gates.tsv", "gate\tbash-gate\tscripts/qa-bash.bash\tbash failed\tcount\n")
	writeFile(t, scripts, "qa-bash.bash", "#!/usr/bin/bash\n")
	if _, err := loadPopulation(scripts, []string{"qa-bash"}); err == nil || !strings.Contains(err.Error(), "found no suites in qa-gates.tsv") {
		t.Fatalf("got %v", err)
	}
	if _, err := loadPopulation(t.TempDir(), nil); err == nil {
		t.Fatal("a missing qa-gates.tsv must be an error")
	}
	writeFile(t, scripts, "qa-gates.tsv", "gate\tbad\n")
	if _, err := loadPopulation(scripts, nil); err == nil || !strings.Contains(err.Error(), ":1:") {
		t.Fatalf("a malformed row must name its line: %v", err)
	}
}

// A suite the table runs whose file is gone would never be picked, silently: it is an error
// naming the suite.
func TestPopulationRefusesASuiteWhoseFileIsGone(t *testing.T) {
	scripts := populationWorld(t)
	writeFile(t, scripts, "qa-gates.tsv", fakeGates+"gate\tgone\tscripts/test-gone.bash\tgone failed\tcount\n")
	_, err := loadPopulation(scripts, nil)
	if err == nil || !strings.Contains(err.Error(), "test-gone.bash") {
		t.Fatalf("got %v", err)
	}
}

func TestGoRowsAreValidatedAgainstThePopulation(t *testing.T) {
	dir := t.TempDir()
	population := []string{"test-alpha.bash", "test-beta.bash"}
	good := writeFile(t, dir, "good.tsv", "# c\nteams\tscripts/test-alpha.bash\nsess\tscripts/test-alpha.bash scripts/test-beta.bash\nfrz\tqa-go\n")
	rows, err := loadGoRows(good, population)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1].suites[1] != "scripts/test-beta.bash" || len(rows[2].suites) != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	for name, body := range map[string]string{
		"misspelt suite":  "teams\tscripts/test-alhpa.bash\n",
		"deleted suite":   "teams\tscripts/test-gone.bash\n",
		"no suites":       "teams\t\n",
		"no tab":          "teams scripts/test-alpha.bash\n",
		"qa-go with more": "teams\tqa-go scripts/test-alpha.bash\n",
	} {
		_, err := loadGoRows(writeFile(t, dir, "bad.tsv", body), population)
		if err == nil || !strings.Contains(err.Error(), ":1:") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, err := loadGoRows(filepath.Join(dir, "missing.tsv"), population); err == nil {
		t.Fatal("a missing table must be an error")
	}
}

// suiteWorld is a scratch root with a scripts directory holding the fake gate table and suites.
func suiteWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	writeFile(t, w.root, "scripts/qa-gates.tsv", fakeGates)
	writeFile(t, w.root, "scripts/go-binary-suites.tsv", "teams\tscripts/test-alpha.bash\nfrz\tqa-go\n")
	for name, body := range map[string]string{
		"test-alpha.bash":               "#!/usr/bin/bash\nuses scripts/lib/helper.bash and tool.py\n",
		"qa-gate-one.bash":              "#!/usr/bin/bash\nmentions main.go\n",
		"test-in-a-string.bash":         "#!/usr/bin/bash\nnothing\n",
		"test-with.dots_and-dash2.bash": "#!/usr/bin/bash\nnames app.go and cmd/app.go\n",
	} {
		writeFile(t, w.root, "scripts/"+name, body)
	}
	return w
}

func (w *world) selectSuites(existing []string, deleted ...string) (*plan, error) {
	w.t.Helper()
	s := w.session()
	if err := s.classify(context.Background(), collected{existing: existing, deleted: deleted}); err != nil {
		w.t.Fatal(err)
	}
	return s.plan, s.selectSuites(context.Background())
}

func TestSuiteIsPickedByNameWithTheReasonAndCoversTheFile(t *testing.T) {
	w := suiteWorld(t)
	w.touch("scripts/lib/helper.bash", "#!/usr/bin/bash")
	p, err := w.selectSuites([]string{"scripts/lib/helper.bash"})
	if err != nil {
		t.Fatal(err)
	}
	want := []pickedSuite{{name: "scripts/test-alpha.bash", why: []string{"names scripts/lib/helper.bash"}}}
	if !reflect.DeepEqual(p.suites, want) || !p.covered["scripts/lib/helper.bash"] {
		t.Fatalf("got %+v covered %v", p.suites, p.covered)
	}
}

func TestSuiteThatItselfChangedRunsItselfOnce(t *testing.T) {
	w := suiteWorld(t)
	p, err := w.selectSuites([]string{"scripts/test-alpha.bash"})
	if err != nil {
		t.Fatal(err)
	}
	want := []pickedSuite{{name: "scripts/test-alpha.bash", why: []string{"the suite itself changed"}}}
	if !reflect.DeepEqual(p.suites, want) {
		t.Fatalf("got %+v", p.suites)
	}
}

func TestEveryChangedFileWithTheSameBasenameIsCreditedAndListed(t *testing.T) {
	w := suiteWorld(t)
	w.touch("src/app.go", "// x")
	w.touch("cmd/app.go", "// x")
	p, err := w.selectSuites([]string{"cmd/app.go", "src/app.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.suites) != 1 || !reflect.DeepEqual(p.suites[0].why, []string{"names cmd/app.go", "names src/app.go"}) {
		t.Fatalf("got %+v", p.suites)
	}
	if !p.covered["cmd/app.go"] || !p.covered["src/app.go"] {
		t.Fatalf("both owners are credited: %v", p.covered)
	}
}

func TestANameInsideALongerChangedNameIsNotHidden(t *testing.T) {
	w := suiteWorld(t)
	w.touch("ccy", "#!/usr/bin/bash")
	w.touch("ccy-teams", "#!/usr/bin/bash")
	writeFile(t, w.root, "scripts/test-alpha.bash", "#!/usr/bin/bash\nruns ccy-teams only\n")
	p, err := w.selectSuites([]string{"ccy", "ccy-teams"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.suites) != 1 || len(p.suites[0].why) != 2 {
		t.Fatalf("the shorter name is a substring of the longer and still named: %+v", p.suites)
	}
}

func TestMarkdownNamesNothing(t *testing.T) {
	w := suiteWorld(t)
	w.touch("tool.md", "# x")
	writeFile(t, w.root, "scripts/test-alpha.bash", "#!/usr/bin/bash\ntool.md\n")
	p, err := w.selectSuites([]string{"tool.md"})
	if err != nil || len(p.suites) != 0 {
		t.Fatalf("got %+v %v", p.suites, err)
	}
}

func TestDeletedFileSelectsTheSuitesThatNameIt(t *testing.T) {
	w := suiteWorld(t)
	p, err := w.selectSuites(nil, "scripts/lib/helper.bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.suites) != 1 || p.suites[0].why[0] != "names scripts/lib/helper.bash" || !p.covered["scripts/lib/helper.bash"] {
		t.Fatalf("got %+v", p.suites)
	}
}

func TestGoClosurePicksTheCommandSuites(t *testing.T) {
	w := suiteWorld(t)
	w.touch("src-go/internal/teams/app.go", "// x")
	w.touch("src-go/internal/other/o.go", "// x")
	w.touch("src-go/go.mod", "module x")
	writeFile(t, w.root, "scripts/test-with.dots_and-dash2.bash", "#!/usr/bin/bash\nnothing\n")
	dirs := filepath.Join(w.root, "src-go/internal/teams") + "\n" + filepath.Join(w.root, "src-go/cmd/teams") + "\n\n"
	w.runner.replies["go list -deps"] = proc.Result{Stdout: dirs}
	p, err := w.selectSuites([]string{"src-go/internal/teams/app.go", "src-go/internal/other/o.go", "src-go/go.mod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.suites) != 1 {
		t.Fatalf("got %+v", p.suites)
	}
	want := []string{"builds from src-go/internal/teams/app.go", "builds from src-go/go.mod"}
	if !reflect.DeepEqual(p.suites[0].why, want) {
		t.Fatalf("got %v want %v", p.suites[0].why, want)
	}
	call := w.runner.calls[len(w.runner.calls)-1]
	if call.Dir != filepath.Join(w.root, "src-go") || call.Argv[len(call.Argv)-1] != "./cmd/teams" {
		t.Fatalf("go list call: %+v", call)
	}
}

func TestGoListIsNotRunWithoutSrcGoFilesOrForGoTestRows(t *testing.T) {
	w := suiteWorld(t)
	w.touch("scripts/x.txt", "x")
	if _, err := w.selectSuites([]string{"scripts/x.txt"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.runner.calls {
		if c.Argv[0] == "go" {
			t.Fatalf("go list ran with no src-go file: %v", c.Argv)
		}
	}
	w.touch("src-go/internal/frz/f.go", "// x")
	writeFile(t, w.root, "scripts/go-binary-suites.tsv", "frz\tqa-go\n")
	if _, err := w.selectSuites([]string{"src-go/internal/frz/f.go"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.runner.calls {
		if c.Argv[0] == "go" {
			t.Fatalf("a qa-go row needs no closure: %v", c.Argv)
		}
	}
}

func TestGoListFailureIsAnError(t *testing.T) {
	w := suiteWorld(t)
	w.touch("src-go/internal/teams/app.go", "// x")
	w.runner.replies["go list -deps"] = proc.Result{Stderr: "no go", Code: 1}
	if _, err := w.selectSuites([]string{"src-go/internal/teams/app.go"}); err == nil || !strings.Contains(err.Error(), "go list -deps ./cmd/teams failed") {
		t.Fatalf("got %v", err)
	}
	s := w.session()
	s.env.Runner = failingRunner{}
	if err := s.classify(context.Background(), collected{existing: []string{"src-go/internal/teams/app.go"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.selectSuites(context.Background()); err == nil {
		t.Fatal("a go that cannot start must be an error")
	}
}

func TestASuiteTheGateTableRunsButWhoseFileIsGoneIsAnError(t *testing.T) {
	w := suiteWorld(t)
	w.touch("tool.py", "# x")
	if err := os.Remove(filepath.Join(w.root, "scripts/qa-gate-one.bash")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.selectSuites([]string{"tool.py"}); err == nil || !strings.Contains(err.Error(), "qa-gate-one.bash") {
		t.Fatalf("got %v", err)
	}
}

func TestUncoveredIsWhatNeitherARuleNorASuiteCovers(t *testing.T) {
	w := suiteWorld(t)
	w.touch("misc/thing.txt", "x")
	w.touch("misc/tool.py", "# x")
	p, err := w.selectSuites([]string{"misc/thing.txt", "misc/tool.py"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.uncovered, []string{"misc/thing.txt"}) {
		t.Fatalf("uncovered: %v", p.uncovered)
	}
}
