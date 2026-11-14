package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// volatileFixtureDir is the repository's fixtures for image-build-arg-volatile.
var volatileFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-image-build-arg-volatile")

func readVolatileFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(volatileFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestImageBuildArgVolatileProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readVolatileFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 9 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := imageBuildArgVolatile([]yamlSource{{rel: "flagged.yml", data: body}})
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestImageBuildArgVolatileIsSilentOnTheCleanFixture(t *testing.T) {
	findings := imageBuildArgVolatile([]yamlSource{{rel: "clean.yml", data: readVolatileFixture(t, "clean.yml")}})
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

// The originating shape (issue 277): a task file reads HEAD and hands it to the build its play
// imports, so the commit is registered in one file and built with in another.
func TestImageBuildArgVolatileFollowsTheCommitIntoAnotherFile(t *testing.T) {
	play := "- hosts: all\n  tasks:\n" +
		"    - name: Read\n      ansible.builtin.command:\n        argv: [git, rev-parse, --verify, \"HEAD^{commit}\"]\n" +
		"      changed_when: false\n      register: src_commit\n" +
		"    - name: Build\n      ansible.builtin.import_tasks: build.yml\n      vars:\n        commit: \"{{ src_commit.stdout }}\"\n"
	tasks := "- name: Build it\n  ansible.builtin.command:\n    argv: [podman, build, --build-arg, \"C={{ commit }}\", /ctx]\n  changed_when: false\n"
	got := imageBuildArgVolatile([]yamlSource{{rel: "tasks/build.yml", data: []byte(tasks)}, {rel: "playbooks/p.yml", data: []byte(play)}})
	if len(got) != 1 || !strings.HasPrefix(got[0], "tasks/build.yml:2: "+ruleBuildArgVolatile+": ") {
		t.Fatalf("got %q", got)
	}
}

func TestImageBuildArgVolatileSkipsAnUnparsedFile(t *testing.T) {
	got := imageBuildArgVolatile([]yamlSource{{rel: "bad.yml", data: []byte("- name: [unclosed\n")}})
	if len(got) != 0 {
		t.Fatalf("an unparsed file is reported by the other rules, not this one: %q", got)
	}
}
