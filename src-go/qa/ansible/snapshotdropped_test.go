package ansible

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// snapshotFixtures is the repository's fixtures for shell-snapshot-dropped-name: a tree whose
// plays deploy includes from its files/ directory, as {{ root_dir }}/files/... does.
var snapshotFixtures = os.DirFS(filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-shell-snapshot-dropped-name"))

// snapshotRun judges one fixture play of the tree.
func snapshotRun(t *testing.T, play string) []string {
	t.Helper()
	body, err := fs.ReadFile(snapshotFixtures, play)
	if err != nil {
		t.Fatal(err)
	}
	return snapshotDroppedNames(snapshotFixtures, []yamlSource{{rel: play, data: body}})
}

// snapshotMarks is the "<rel>:<line>" of every line marked EXPECT-FINDING in the named files.
func snapshotMarks(t *testing.T, rels ...string) []string {
	t.Helper()
	var want []string
	for _, rel := range rels {
		body, err := fs.ReadFile(snapshotFixtures, rel)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
				want = append(want, rel+":"+strconv.Itoa(i+1))
			}
		}
	}
	return want
}

// snapshotPlaces is the "<rel>:<line>" of every finding, each checked to carry the rule ID.
func snapshotPlaces(t *testing.T, findings []string) []string {
	t.Helper()
	places := make([]string, 0, len(findings))
	for _, f := range findings {
		m := findingPlace.FindStringSubmatch(f)
		if m == nil || m[2] != ruleSnapshotDropped {
			t.Fatalf("finding %q is not <rel>:<line>: %s: ...", f, ruleSnapshotDropped)
		}
		places = append(places, m[1])
	}
	return places
}

func TestSnapshotDroppedProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	findings := snapshotRun(t, "flagged.yml")
	want := snapshotMarks(t, "flagged.yml", "files/flagged-include.bash.j2", "files/flagged-loop.bash")
	if len(want) < 9 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	got := snapshotPlaces(t, findings)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("finding places\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestSnapshotDroppedIsSilentOnTheCleanFixture(t *testing.T) {
	if findings := snapshotRun(t, "clean.yml"); len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestSnapshotDroppedNamesTheDroppedName(t *testing.T) {
	play := "- hosts: localhost\n  tasks:\n    - ansible.builtin.copy:\n        content: |\n" +
		"          g() { _h; echo \"$V\"; }\n          V=1\n        dest: /etc/profile.d/x.sh\n"
	got := snapshotDroppedNames(fstest.MapFS{}, []yamlSource{{rel: "p.yml", data: []byte(play)}})
	if len(got) != 2 || !strings.HasPrefix(got[0], "p.yml:5: "+ruleSnapshotDropped+": ") || !strings.Contains(got[0], "_h") ||
		!strings.Contains(got[1], "V") {
		t.Fatalf("got %q", got)
	}
}

func TestSnapshotDroppedReportsAnUnreadableSource(t *testing.T) {
	play := "- hosts: localhost\n  tasks:\n    - ansible.builtin.copy:\n        src: \"{{ root_dir }}/files/missing.bash\"\n" +
		"        dest: /home/u/.bashrc-includes/missing.bash\n"
	got := snapshotDroppedNames(fstest.MapFS{}, []yamlSource{{rel: "p.yml", data: []byte(play)}})
	if len(got) != 1 || !strings.HasPrefix(got[0], "p.yml:3: "+ruleSnapshotDropped+": ") || !strings.Contains(got[0], "cannot") {
		t.Fatalf("got %q", got)
	}
}

func TestSnapshotDroppedReportsAnIncludeThatDoesNotParse(t *testing.T) {
	play := "- hosts: localhost\n  tasks:\n    - ansible.builtin.blockinfile:\n        path: /home/u/.bashrc\n" +
		"        block: |\n          f() {\n"
	got := snapshotDroppedNames(fstest.MapFS{}, []yamlSource{{rel: "p.yml", data: []byte(play)}})
	if len(got) != 1 || !strings.Contains(got[0], "cannot") {
		t.Fatalf("got %q", got)
	}
}

func TestSnapshotDroppedSkipsAFileThatIsNotYAML(t *testing.T) {
	if got := snapshotDroppedNames(fstest.MapFS{}, []yamlSource{{rel: "bad.yml", data: []byte("a: [\n")}}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
