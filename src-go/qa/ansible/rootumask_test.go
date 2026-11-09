package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cacheFixtureDir is the repository's fixtures for the rule (src-go/qa/ansible is three below the root).
var cacheFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-root-umask")

func readCacheFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(cacheFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRootUmaskProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readCacheFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 21 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := rootUmask("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestRootUmaskIsSilentOnTheCleanFixture(t *testing.T) {
	findings := rootUmask("clean.yml", readCacheFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestRootUmaskFollowsThePlaysBecome(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       int
	}{
		{"a become play", "- hosts: all\n  become: true\n  handlers:\n    - name: h\n      command: fc-cache -f\n", 1},
		{"a play without become runs as the connecting user", "- hosts: all\n  tasks:\n    - command: fc-cache -f\n", 0},
		{"a become play, the task as a user", "- hosts: all\n  become: true\n  tasks:\n    - become_user: u\n      command: fc-cache -f\n", 0},
		{"a plain play, the task escalated", "- hosts: all\n  tasks:\n    - become: true\n      command: fc-cache -f\n", 1},
		{"a become play, the task de-escalated", "- hosts: all\n  become: yes\n  pre_tasks:\n    - become: false\n      command: fc-cache -f\n", 0},
		{"a templated become is taken as root", "- hosts: all\n  become: \"{{ b }}\"\n  post_tasks:\n    - command: fc-cache -f\n", 1},
		{"a become play to root by name", "- hosts: all\n  become: true\n  become_user: root\n  tasks:\n    - command: fc-cache -f\n", 1},
	}
	for _, c := range cases {
		if got := len(rootUmask("p.yml", []byte(c.yaml))); got != c.want {
			t.Errorf("%s: %d findings, want %d", c.name, got, c.want)
		}
	}
}

func TestRootUmaskNamesTheToolAndTheFix(t *testing.T) {
	got := rootUmask("p.yml", []byte("- command:\n    argv: [fc-cache, -f]\n"))
	want := []string{"p.yml:1: fc-cache runs as root under the inherited umask, so a restrictive one (0077) " +
		"leaves the files it writes unreadable to users; run it as a shell command that starts umask 022 &&"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestRootUmaskResolvesADestinationFromThePlayVars(t *testing.T) {
	play := "- hosts: all\n  vars:\n    src_dir: %s\n  tasks:\n    - become: true\n      ansible.builtin.git:\n" +
		"        repo: r\n        dest: \"{{ src_dir }}\"\n"
	if got := rootUmask("p.yml", []byte(strings.Replace(play, "%s", "/opt/x", 1))); len(got) != 1 {
		t.Fatalf("/opt destination: %q", got)
	}
	if got := rootUmask("p.yml", []byte(strings.Replace(play, "%s", "/var/lib/x", 1))); len(got) != 0 {
		t.Fatalf("/var/lib destination: %q", got)
	}
}

func TestRootUmaskNamesTheWrittenPathAndTheFix(t *testing.T) {
	got := rootUmask("p.yml", []byte("- shell: wget -qO /usr/bin/yq u && chmod +x /usr/bin/yq\n"))
	want := []string{"p.yml:1: /usr/bin/yq is written as root under the inherited umask, so a restrictive one (0077) " +
		"leaves it unreadable to users; set umask 022 first, or chmod it to a mode others can read"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestRootUmaskReportsAFileThatIsNotYAML(t *testing.T) {
	got := rootUmask("bad.yml", []byte("a: [\n"))
	if len(got) != 1 || !strings.HasPrefix(got[0], "bad.yml:") || !strings.Contains(got[0], "cannot be parsed as YAML") {
		t.Fatalf("got %q", got)
	}
}
