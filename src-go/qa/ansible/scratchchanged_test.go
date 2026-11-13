package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// scratchFixtureDir is the repository's fixtures for scratch-always-changed.
var scratchFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-scratch-always-changed")

func readScratchFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(scratchFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestScratchAlwaysChangedProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readScratchFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 8 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := scratchAlwaysChanged([]yamlSource{{rel: "flagged.yml", data: body}})
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestScratchAlwaysChangedIsSilentOnTheCleanFixture(t *testing.T) {
	findings := scratchAlwaysChanged([]yamlSource{{rel: "clean.yml", data: readScratchFixture(t, "clean.yml")}})
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

// The originating shape: a play hands its tempfile's path to an imported task file through
// vars, and the task file writes under it (play-teamchat.yml, teamchat-build-go-commands.yml).
func TestScratchAlwaysChangedFollowsAVariableIntoAnotherFile(t *testing.T) {
	play := "- hosts: all\n  tasks:\n" +
		"    - name: Make it\n      ansible.builtin.tempfile:\n        state: directory\n      register: tc_dir\n      changed_when: false\n" +
		"    - name: Build\n      ansible.builtin.import_tasks: build.yml\n      vars:\n        cross_dir: \"{{ tc_dir.path }}\"\n"
	tasks := "- name: Per arch\n  ansible.builtin.file:\n    path: \"{{ cross_dir }}/{{ item }}\"\n    state: directory\n    mode: \"0700\"\n"
	got := scratchAlwaysChanged([]yamlSource{{rel: "tasks/build.yml", data: []byte(tasks)}, {rel: "playbooks/p.yml", data: []byte(play)}})
	want := []string{"tasks/build.yml:2: " + ruleScratchChanged + ": this task only makes, fills or removes a scratch " +
		"directory and reports changed on every run it makes; set changed_when: false (the task that installs from it reports the change)"}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestScratchAlwaysChangedSkipsAnUnparsedFile(t *testing.T) {
	got := scratchAlwaysChanged([]yamlSource{{rel: "bad.yml", data: []byte("- name: [unclosed\n")}})
	if len(got) != 0 {
		t.Fatalf("an unparsed file is reported by the other rules, not this one: %q", got)
	}
}
