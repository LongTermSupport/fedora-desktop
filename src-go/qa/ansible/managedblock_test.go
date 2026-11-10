package ansible

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// managedBlockFixtureDir is the repository's fixtures for duplicate-managed-block.
var managedBlockFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-duplicate-managed-block")

// readManagedBlockFixtures reads every file of one fixture set, in name order.
func readManagedBlockFixtures(t *testing.T, set string) []yamlSource {
	t.Helper()
	dir := filepath.Join(managedBlockFixtureDir, set)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]yamlSource, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			t.Fatalf("%s in the fixture set is not a regular file", entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, yamlSource{rel: set + "/" + entry.Name(), data: body})
	}
	return sources
}

// markedPlaces is the "<rel>:<line>" of every line marked EXPECT-FINDING.
func markedPlaces(sources []yamlSource) []string {
	var want []string
	for _, src := range sources {
		for i, line := range strings.Split(string(src.data), "\n") {
			if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
				want = append(want, src.rel+":"+strconv.Itoa(i+1))
			}
		}
	}
	return want
}

// findingPlace splits a finding into its "<rel>:<line>" and its rule ID.
var findingPlace = regexp.MustCompile(`^([^:]+:[0-9]+): ([a-z-]+): `)

// findingPlaces is the "<rel>:<line>" of every finding.
func findingPlaces(t *testing.T, findings []string) []string {
	t.Helper()
	places := make([]string, 0, len(findings))
	for _, f := range findings {
		m := findingPlace.FindStringSubmatch(f)
		if m == nil || m[2] != ruleManagedBlock {
			t.Fatalf("finding %q is not <rel>:<line>: %s: ...", f, ruleManagedBlock)
		}
		places = append(places, m[1])
	}
	return places
}

func TestDuplicateManagedBlockProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	sources := readManagedBlockFixtures(t, "flagged")
	want := markedPlaces(sources)
	if len(want) < 12 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := duplicateManagedBlocks(sources)
	got := findingPlaces(t, findings)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("finding places\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestDuplicateManagedBlockIsSilentOnTheCleanFixture(t *testing.T) {
	if findings := duplicateManagedBlocks(readManagedBlockFixtures(t, "clean")); len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestDuplicateManagedBlockNamesTheOtherOwner(t *testing.T) {
	a := yamlSource{rel: "playbooks/a.yml", data: []byte("- hosts: x\n  tasks:\n    - blockinfile:\n        path: /f\n        block: one\n")}
	b := yamlSource{rel: "tasks/b.yml", data: []byte("- blockinfile:\n    path: /f\n    block: two\n")}
	got := duplicateManagedBlocks([]yamlSource{a, b})
	want := []string{
		"playbooks/a.yml:3: " + ruleManagedBlock + ": the block \"# {mark} ANSIBLE MANAGED BLOCK\" in /f is also " +
			"managed by tasks/b.yml:1 with different content, so each run rewrites the other's; give it one owner",
		"tasks/b.yml:1: " + ruleManagedBlock + ": the block \"# {mark} ANSIBLE MANAGED BLOCK\" in /f is also " +
			"managed by playbooks/a.yml:3 with different content, so each run rewrites the other's; give it one owner",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestDuplicateManagedBlockSkipsAFileThatIsNotYAML(t *testing.T) {
	got := duplicateManagedBlocks([]yamlSource{{rel: "bad.yml", data: []byte("a: [\n")}})
	if len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
