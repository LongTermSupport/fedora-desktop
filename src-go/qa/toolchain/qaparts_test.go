package toolchain

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
)

// qaParts is what this test reads of qa.yml: the parts the QA runs in, and the verdict job.
type qaParts struct {
	Jobs struct {
		Part struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]string `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"qa-part"`
		Verdict struct {
			Needs []string `yaml:"needs"`
			If    string   `yaml:"if"`
		} `yaml:"qa"`
	} `yaml:"jobs"`
}

// partFindings judges the parts' lists for one mode (the matrix key "full" or "diff"): every
// part but one selects --only, the one other --except, and the --except list is exactly the
// union of the --only lists with no name twice. Then the parts run every gate exactly once,
// whatever the table holds; a part that is missing, or a list edited on one side alone, leaves a
// gate run by no part or by two.
func partFindings(include []map[string]string, mode string) []string {
	var findings, only, except []string
	excepts := 0
	for _, part := range include {
		names := strings.Split(part[mode], ",")
		switch part["select"] {
		case "--only":
			for _, name := range names {
				if slices.Contains(only, name) {
					findings = append(findings, fmt.Sprintf("%s: %s is in two --only parts", mode, name))
				}
				only = append(only, name)
			}
		case "--except":
			excepts++
			except = names
		default:
			findings = append(findings, fmt.Sprintf("%s: part %q selects %q, not --only or --except", mode, part["part"], part["select"]))
		}
	}
	if excepts != 1 {
		return append(findings, fmt.Sprintf("%s: %d parts take --except; exactly one takes the rest", mode, excepts))
	}
	if !slices.Equal(slices.Sorted(slices.Values(only)), slices.Sorted(slices.Values(except))) {
		findings = append(findings, fmt.Sprintf("%s: --except %v is not the --only parts' %v, so a gate runs in no part or two", mode, except, only))
	}
	return findings
}

// The QA runs in parallel parts (qa.yml, job qa-part), and the run must still judge every gate:
// each mode's lists partition the table, every --only name of the full run is a gate of
// qa-gates.tsv, and the one verdict job needs the parts and runs after a failed one.
func TestTheQAPartsRunEveryGateOnce(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(os.DirFS(root), qaWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	var wf qaParts
	if err = yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	include := wf.Jobs.Part.Strategy.Matrix.Include
	if len(include) < 2 {
		t.Fatalf("qa.yml's qa-part has %d parts", len(include))
	}
	for _, mode := range []string{"full", "diff"} {
		for _, f := range partFindings(include, mode) {
			t.Error(f)
		}
	}
	table, err := gates.Load(filepath.Join(root, "scripts", "qa-gates.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range include {
		for _, name := range strings.Split(part["full"], ",") {
			if !slices.ContainsFunc(table, func(g gates.Gate) bool { return g.Name == name }) {
				t.Errorf("part %s names %q, which is no gate of qa-gates.tsv", part["part"], name)
			}
		}
	}
	if !slices.Contains(wf.Jobs.Verdict.Needs, "qa-part") || !strings.Contains(wf.Jobs.Verdict.If, "!cancelled()") {
		t.Errorf("the qa job must need qa-part and run after a failed part (needs %v, if %q)", wf.Jobs.Verdict.Needs, wf.Jobs.Verdict.If)
	}
}

// Each way the lists can stop partitioning the table is found.
func TestEachBrokenPartitionIsFound(t *testing.T) {
	part := func(name, sel, list string) map[string]string {
		return map[string]string{"part": name, "select": sel, "full": list}
	}
	good := []map[string]string{part("go", "--only", "go"), part("h", "--only", "helper-tests"), part("rest", "--except", "go,helper-tests")}
	if got := partFindings(good, "full"); len(got) != 0 {
		t.Fatalf("a partition was refused: %v", got)
	}
	for name, include := range map[string][]map[string]string{
		"an --only part dropped":     {part("go", "--only", "go"), part("rest", "--except", "go,helper-tests")},
		"a name only in --except":    {part("go", "--only", "go"), part("rest", "--except", "go,docs")},
		"a name in two parts":        {part("go", "--only", "go"), part("g2", "--only", "go"), part("rest", "--except", "go")},
		"no rest part":               {part("go", "--only", "go")},
		"two rest parts":             {part("a", "--except", "go"), part("b", "--except", "go"), part("go", "--only", "go")},
		"a selector that is no flag": {part("go", "--all", "go"), part("rest", "--except", "go")},
	} {
		if got := partFindings(include, "full"); len(got) == 0 {
			t.Errorf("%s: no finding", name)
		}
	}
}
