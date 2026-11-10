package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// alwaysChangedFixtureDir is the repository's fixtures for command-always-changed.
var alwaysChangedFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-command-always-changed")

func readAlwaysChangedFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(alwaysChangedFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCommandAlwaysChangedProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readAlwaysChangedFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 14 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := commandAlwaysChanged("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestCommandAlwaysChangedIsSilentOnTheCleanFixture(t *testing.T) {
	findings := commandAlwaysChanged("clean.yml", readAlwaysChangedFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestCommandAlwaysChangedReadsATaskListOutsideAPlay(t *testing.T) {
	list := "- name: In tasks/\n  ansible.builtin.command: thing\n" +
		"- name: Probe\n  ansible.builtin.command: other\n  changed_when: false\n"
	got := commandAlwaysChanged("tasks/x.yml", []byte(list))
	want := []string{"tasks/x.yml:2: " + ruleAlwaysChanged + ": this command task reports changed on every run it makes, " +
		"whether it changed anything or not; set changed_when from what the command printed or a check made before it, " +
		"or changed_when: false if it changes nothing"}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestTheScanReportsCommandAlwaysChangedOnceAndAnUnparsedFileOnce(t *testing.T) {
	fsys := fstest.MapFS{
		"tasks/a.yml":   {Data: []byte("- ansible.builtin.command: thing\n")},
		"tasks/bad.yml": {Data: []byte("a: [\n")},
	}
	var f Findings
	if err := scanPlayUX(fsys, []string{"tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.PlayUX) != 2 || !strings.Contains(f.PlayUX[0], "tasks/a.yml:1: "+ruleAlwaysChanged+": ") ||
		!strings.Contains(f.PlayUX[1], "tasks/bad.yml:") {
		t.Fatalf("got %q", f.PlayUX)
	}
	if !slices.Contains(f.Out, "    Why and how to fix: CLAUDE/QA.md#"+ruleAlwaysChanged) {
		t.Fatalf("no pointer to the rule's documentation: %q", f.Out)
	}
}

func TestCommandAlwaysChangedReportsAFileThatIsNotYAML(t *testing.T) {
	got := commandAlwaysChanged("bad.yml", []byte("a: [\n"))
	if len(got) != 1 || !strings.Contains(got[0], "cannot be parsed as YAML") {
		t.Fatalf("got %q", got)
	}
}
