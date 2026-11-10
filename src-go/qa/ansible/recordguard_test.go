package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// recordGuardFixtureDir is the repository's fixtures for install-guard-trusts-record.
var recordGuardFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-install-guard-record")

func readRecordGuardFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(recordGuardFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestInstallGuardTrustsRecordProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readRecordGuardFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 4 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := installGuardTrustsRecord("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestInstallGuardTrustsRecordIsSilentOnTheCleanFixture(t *testing.T) {
	findings := installGuardTrustsRecord("clean.yml", readRecordGuardFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestInstallGuardTrustsRecordNamesTheUnguardedArtefact(t *testing.T) {
	list := "- name: Read\n  ansible.builtin.stat:\n    path: /r\n  register: rec\n" +
		"- name: Install\n  ansible.builtin.copy:\n    src: a\n    dest: /bin/a\n  when: not rec.stat.exists\n" +
		"- name: Record\n  ansible.builtin.copy:\n    content: x\n    dest: /r\n  when: not rec.stat.exists\n"
	got := installGuardTrustsRecord("tasks/x.yml", []byte(list))
	if len(got) != 1 || !strings.HasPrefix(got[0], "tasks/x.yml:2: "+ruleRecordGuard+": ") ||
		!strings.Contains(got[0], "/bin/a") || !strings.Contains(got[0], "/r") {
		t.Fatalf("got %q", got)
	}
}

func TestInstallGuardTrustsRecordReadsAStatInALoopAsNoGuard(t *testing.T) {
	list := "- name: Read\n  ansible.builtin.stat:\n    path: \"{{ item }}\"\n  loop: [/r]\n  register: rec\n" +
		"- name: Install\n  ansible.builtin.copy:\n    src: a\n    dest: /bin/a\n  when: not rec.stat.exists\n"
	if got := installGuardTrustsRecord("tasks/x.yml", []byte(list)); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestInstallGuardTrustsRecordReportsAFileThatIsNotYAMLAsNothing(t *testing.T) {
	if got := installGuardTrustsRecord("bad.yml", []byte("a: [\n")); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestTheScanReportsInstallGuardTrustsRecord(t *testing.T) {
	fsys := fstest.MapFS{"tasks/a.yml": {Data: readRecordGuardFixture(t, "flagged.yml")}}
	var f Findings
	if err := scanPlayUX(fsys, []string{"tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.PlayUX, func(v string) bool { return strings.Contains(v, ": "+ruleRecordGuard+": ") }) {
		t.Fatalf("got %q", f.PlayUX)
	}
	if !slices.Contains(f.Out, "    Why and how to fix: CLAUDE/QA.md#"+ruleRecordGuard) {
		t.Fatalf("no pointer to the rule's documentation: %q", f.Out)
	}
}
