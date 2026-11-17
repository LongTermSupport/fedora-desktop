package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// pipxNameFixtureDir is the repository's fixtures for pipx-name-not-canonical.
var pipxNameFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-pipx-name-not-canonical")

func readPipxNameFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(pipxNameFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPipxNameNotCanonicalProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readPipxNameFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 12 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := pipxNameNotCanonical("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestPipxNameNotCanonicalIsSilentOnTheCleanFixture(t *testing.T) {
	findings := pipxNameNotCanonical("clean.yml", readPipxNameFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestPipxNameNotCanonicalNamesTheCanonicalForm(t *testing.T) {
	list := "- community.general.pipx:\n    name: Hugging__Face.hub>=1\n    state: latest\n"
	got := pipxNameNotCanonical("tasks/x.yml", []byte(list))
	want := []string{"tasks/x.yml:1: " + rulePipxName + ": pipx names this app's venv \"hugging-face-hub\" but the " +
		"module looks for \"Hugging__Face.hub\", so it never sees the app installed: it reinstalls and reports changed " +
		"on every run, and absent removes nothing; write the name as \"hugging-face-hub\""}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestPipxCanonicalName(t *testing.T) {
	for in, want := range map[string]string{
		"huggingface_hub": "huggingface-hub", "PyYAML": "pyyaml", "a._-b": "a-b", "pdm": "pdm", "zope.interface": "zope-interface",
	} {
		if got := pipxCanonical(in); got != want {
			t.Errorf("pipxCanonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPipxRequirementName(t *testing.T) {
	for in, want := range map[string]string{
		"pdm": "pdm", " pdm ": "pdm", "zope.interface>=6": "zope.interface", "jupyter_core[test]": "jupyter_core",
		"a ; python_version>'3'": "a", "pkg @ https://example.com/x.whl": "pkg", "x~=1.0": "x", "y!=2": "y",
	} {
		if got := requirementName(in); got != want {
			t.Errorf("requirementName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTheScanReportsPipxNameNotCanonical(t *testing.T) {
	fsys := fstest.MapFS{
		"tasks/a.yml": {Data: []byte("- community.general.pipx:\n    name: huggingface_hub\n    state: latest\n")},
	}
	var f Findings
	if err := scanPlayUX(fsys, []string{"tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.PlayUX) != 1 || !strings.HasPrefix(f.PlayUX[0], "tasks/a.yml:1: "+rulePipxName+": ") {
		t.Fatalf("got %q", f.PlayUX)
	}
	if !slices.Contains(f.Out, "    Why and how to fix: CLAUDE/QA.md#"+rulePipxName) {
		t.Fatalf("no pointer to the rule's documentation: %q", f.Out)
	}
}

func TestPipxNameNotCanonicalSkipsAFileThatIsNotYAML(t *testing.T) {
	if got := pipxNameNotCanonical("bad.yml", []byte("a: [\n")); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
