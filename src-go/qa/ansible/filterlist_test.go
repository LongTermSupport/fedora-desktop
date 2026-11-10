package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// filterListFixtureDir is the repository's fixtures for filter-list-unchecked.
var filterListFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-filter-list-unchecked")

func readFilterListFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(filterListFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFilterListUncheckedProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readFilterListFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 6 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := filterListUnchecked("flagged.yml", body, map[string]bool{})
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestFilterListUncheckedIsSilentOnTheCleanFixture(t *testing.T) {
	findings := filterListUnchecked("clean.yml", readFilterListFixture(t, "clean.yml"), map[string]bool{})
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestFilterListUncheckedNamesTheListAndWhatItPicksFrom(t *testing.T) {
	list := "- name: Pick\n  ansible.builtin.debug:\n    msg: \"{{ teams | selectattr('team', 'in', wanted) | list }}\"\n"
	got := filterListUnchecked("tasks/x.yml", []byte(list), map[string]bool{})
	if len(got) != 1 || !strings.HasPrefix(got[0], "tasks/x.yml:3: "+ruleFilterList+": ") ||
		!strings.Contains(got[0], "wanted") || !strings.Contains(got[0], "teams") {
		t.Fatalf("got %q", got)
	}
}

func TestFilterListUncheckedTakesARepositoryVariableAsNoInput(t *testing.T) {
	list := "- name: Pick\n  ansible.builtin.debug:\n    msg: \"{{ teams | selectattr('team', 'in', wanted) | list }}\"\n"
	if got := filterListUnchecked("tasks/x.yml", []byte(list), map[string]bool{"wanted": true}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestFilterListUncheckedReportsAFileThatIsNotYAMLAsNothing(t *testing.T) {
	if got := filterListUnchecked("bad.yml", []byte("a: [\n"), map[string]bool{}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestTheScanReportsFilterListUnchecked(t *testing.T) {
	fsys := fstest.MapFS{"tasks/a.yml": {Data: readFilterListFixture(t, "flagged.yml")}}
	var f Findings
	if err := scanPlayUX(fsys, []string{"tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.PlayUX, func(v string) bool { return strings.Contains(v, ": "+ruleFilterList+": ") }) {
		t.Fatalf("got %q", f.PlayUX)
	}
	if !slices.Contains(f.Out, "    Why and how to fix: CLAUDE/QA.md#"+ruleFilterList) {
		t.Fatalf("no pointer to the rule's documentation: %q", f.Out)
	}
}
