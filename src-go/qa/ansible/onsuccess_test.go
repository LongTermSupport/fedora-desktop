package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// onSuccessFixtureDir is the repository's fixtures for changed-when-on-success.
var onSuccessFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-changed-when-on-success")

func readOnSuccessFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(onSuccessFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestChangedWhenOnSuccessProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readOnSuccessFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 14 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := changedWhenOnSuccess("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestChangedWhenOnSuccessIsSilentOnTheCleanFixture(t *testing.T) {
	findings := changedWhenOnSuccess("clean.yml", readOnSuccessFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestChangedWhenOnSuccessReadsATaskListOutsideAPlay(t *testing.T) {
	list := "- name: In tasks/\n  ansible.builtin.command: thing\n  register: r\n  changed_when: r.rc == 0\n"
	got := changedWhenOnSuccess("tasks/x.yml", []byte(list))
	want := []string{"tasks/x.yml:2: " + ruleChangedOnSuccess + ": this changed_when is true on every run the task " +
		"succeeds, so the task reports changed whether it changed anything or not; decide changed from what the task " +
		"observed (its output, or a check made before it), or changed_when: false if it changes nothing"}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestTheScanReportsChangedWhenOnSuccessWithItsPointer(t *testing.T) {
	fsys := fstest.MapFS{
		"tasks/a.yml": {Data: []byte("- ansible.builtin.command: thing\n  register: r\n  changed_when: r is succeeded\n")},
	}
	var f Findings
	if err := scanPlayUX(fsys, []string{"tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.PlayUX) != 1 || !strings.Contains(f.PlayUX[0], "tasks/a.yml:1: "+ruleChangedOnSuccess+": ") {
		t.Fatalf("got %q", f.PlayUX)
	}
	if !slices.Contains(f.Out, "    Why and how to fix: CLAUDE/QA.md#"+ruleChangedOnSuccess) {
		t.Fatalf("no pointer to the rule's documentation: %q", f.Out)
	}
}

func TestChangedWhenOnSuccessSplitsOnlyOutsideParenthesesAndQuotes(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"r.rc == 0", true},
		{"(r.rc == 0)", true},
		{"'a or b' in r.stdout", false},
		{"(x or r.rc == 0) and y", false},
		{"(x or r.rc == 0) and r is not skipped", true},
		{"not (r is failed)", true},
		{"r.rc|int == 0", false},
		{"r.failed == false", true},
	} {
		if got := trueOnSuccess(tc.expr, "r", true, true); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.expr, got, tc.want)
		}
	}
}
