package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// checkoutHomeFixtureDir is the repository's fixtures for checkout-by-home-path.
var checkoutHomeFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-checkout-by-home-path")

func readCheckoutHomeFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(checkoutHomeFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCheckoutByHomePathProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	body := readCheckoutHomeFixture(t, "flagged.yml")
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	if len(want) < 5 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings := checkoutByHomePath("flagged.yml", body)
	if got := findingLines(t, "flagged.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestCheckoutByHomePathIsSilentOnTheCleanFixture(t *testing.T) {
	findings := checkoutByHomePath("clean.yml", readCheckoutHomeFixture(t, "clean.yml"))
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

func TestCheckoutByHomePathNamesTheFix(t *testing.T) {
	got := checkoutByHomePath("playbooks/p.yml", []byte("x:\n  repo: ~/Projects/fedora-desktop\n"))
	want := []string{"playbooks/p.yml:2: " + ruleCheckoutHome + ": names the checkout by its usual clone path, " +
		"so a play run from any other checkout acts on that one instead; use \"{{ root_dir }}\""}
	if !slices.Equal(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestTheScanReportsCheckoutByHomePath(t *testing.T) {
	fsys := fstest.MapFS{
		"playbooks/p.yml": {Data: []byte("#!/usr/bin/env ansible-playbook\n---\n- hosts: desktop\n  vars:\n" +
			"    repo_path: \"{{ lookup('env', 'HOME') }}/Projects/fedora-desktop\"\n  tasks: []\n"), Mode: 0o755},
	}
	var f Findings
	if err := scanPlayUX(fsys, []string{"playbooks"}, &f); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.PlayUX, func(v string) bool { return strings.HasPrefix(v, "playbooks/p.yml:5: "+ruleCheckoutHome+": ") }) {
		t.Fatalf("the scan did not report the rule:\n%s", strings.Join(f.PlayUX, "\n"))
	}
}
