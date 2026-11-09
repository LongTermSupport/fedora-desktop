package proto

import (
	"strings"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func mustRef(t *testing.T, s string) Ref {
	t.Helper()
	r, ok := ParseRef(s)
	if !ok {
		t.Fatalf("ParseRef(%q) refused", s)
	}
	return r
}

func TestOwnerAndRepoGrammar(t *testing.T) {
	for _, ok := range []string{"a", "example-org", "a1", strings.Repeat("a", 39)} {
		if !ValidOwner(ok) {
			t.Errorf("ValidOwner(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "A", "a_b", strings.Repeat("a", 40)} {
		if ValidOwner(bad) {
			t.Errorf("ValidOwner(%q) = true", bad)
		}
	}
	for _, ok := range []string{"o/r", "example-org/my.repo_x-1", "o/" + strings.Repeat("r", 100), "o/.github", "o/a.gitx"} {
		if !ValidOwnerRepo(ok) {
			t.Errorf("ValidOwnerRepo(%q) = false", ok)
		}
	}
	for _, bad := range []string{"o", "o/", "/r", "o/.", "o/..", "o/r.git", "O/r", "o/R", "o/r/x", "o/" + strings.Repeat("r", 101)} {
		if ValidOwnerRepo(bad) {
			t.Errorf("ValidOwnerRepo(%q) = true", bad)
		}
	}
}

func TestPathPrefixes(t *testing.T) {
	for _, ok := range []string{"docs/", "docs/a.md", "a/b/c/", "README"} {
		if !ValidPathPrefix(ok) {
			t.Errorf("ValidPathPrefix(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "/", "docs//", "../x", "a/./b", "/docs/", strings.Repeat("a/", 9)} {
		if ValidPathPrefix(bad) {
			t.Errorf("ValidPathPrefix(%q) = true", bad)
		}
	}
	prefixes := []string{"docs/", "README"}
	for path, want := range map[string]bool{"docs/a.md": true, "docs/x/y": true, "README": true, "README.md": false, "docsx": false} {
		if PathAllowed(path, prefixes) != want {
			t.Errorf("PathAllowed(%q) = %v", path, !want)
		}
	}
	if !PathAllowed("anything", nil) {
		t.Fatal("no prefixes must allow any path")
	}
}

func TestParseRef(t *testing.T) {
	cases := map[string]Ref{
		"path:o/r@" + testSHA + ":docs/a.md": {Form: RefPath, Owner: "o", Repo: "r", SHA: testSHA, Path: "docs/a.md"},
		"commit:o/r@" + testSHA:              {Form: RefCommit, Owner: "o", Repo: "r", SHA: testSHA},
		"pr:o/r#12@" + testSHA:               {Form: RefPR, Owner: "o", Repo: "r", Num: 12, SHA: testSHA},
		"issue:o/r#1":                        {Form: RefIssue, Owner: "o", Repo: "r", Num: 1},
	}
	for s, want := range cases {
		got := mustRef(t, s)
		want.Text = s
		if got != want {
			t.Errorf("ParseRef(%q) = %+v; want %+v", s, got, want)
		}
		if got.OwnerRepo() != "o/r" {
			t.Errorf("OwnerRepo = %q", got.OwnerRepo())
		}
	}
	long := "path:o/r@" + testSHA + ":" + strings.Repeat(strings.Repeat("a", 64)+"/", 7) + strings.Repeat("a", 64)
	if mustRef(t, long); len(long) > MaxRefLen {
		t.Fatalf("test reference of %d characters is over the cap", len(long))
	}
	tooLong := "path:" + strings.Repeat("o", 39) + "/" + strings.Repeat("r", 100) + "@" + testSHA + ":" + strings.Repeat(strings.Repeat("a", 64)+"/", 7) + strings.Repeat("a", 64)
	for _, bad := range []string{
		"", "commit:o/r", "commit:o/r@" + strings.ToUpper(testSHA), "commit:O/r@" + testSHA,
		"pr:o/r#0@" + testSHA, "pr:o/r#12", "issue:o/r#01", "issue:o/r#12345678901",
		"path:o/r@" + testSHA + ":", "path:o/r@" + testSHA + ":a/../b", "path:o/r@" + testSHA + ":./a",
		"path:o/r@" + testSHA + ":" + strings.Repeat("a/", 8) + "a", "commit:o/r.git@" + testSHA,
		"commit:o/..@" + testSHA, "branch:o/r@" + testSHA, "commit:o/r@" + testSHA + " ", tooLong,
	} {
		if r, ok := ParseRef(bad); ok {
			t.Errorf("ParseRef(%q) = %+v, want refused", bad, r)
		}
	}
}

func TestLowercaseRefRepo(t *testing.T) {
	cases := map[string]string{
		"commit:Example-Org/My.Repo@" + testSHA:     "commit:example-org/my.repo@" + testSHA,
		"pr:O/R#1@" + testSHA:                       "pr:o/r#1@" + testSHA,
		"path:O/R@" + testSHA + ":Docs/README.md":   "path:o/r@" + testSHA + ":Docs/README.md",
		"issue:O/R#3":                               "issue:o/r#3",
		"not a ref":                                 "not a ref",
		"commit:" + string(rune(0x212a)) + "/r@abc": "commit:" + string(rune(0x212a)) + "/r@abc",
	}
	for in, want := range cases {
		if got := LowercaseRefRepo(in); got != want {
			t.Errorf("LowercaseRefRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefTrustClass(t *testing.T) {
	forge := Forge{
		Kind: ForgeGitHub, Org: "org",
		Repos:   []ForgeRepo{{Repo: "org/listed", Branches: []string{"main"}}, {Repo: "other/listed"}},
		Exclude: []string{"org/secret", "other/x"},
	}
	cases := map[string]RefClass{
		"org/listed":   RefListed,
		"other/listed": RefListed,
		"org/any":      RefOrg,
		"org/secret":   RefExcluded,
		"other/x":      RefExcluded,
		"third/y":      RefExternal,
	}
	for repo, want := range cases {
		if got := forge.Class(mustRef(t, "commit:"+repo+"@"+testSHA)); got != want {
			t.Errorf("Class(%s) = %v, want %v", repo, got, want)
		}
	}
	if b := forge.ListedBranches(mustRef(t, "commit:org/listed@"+testSHA)); len(b) != 1 || b[0] != "main" {
		t.Fatalf("ListedBranches = %v", b)
	}
	if b := forge.ListedBranches(mustRef(t, "commit:other/listed@"+testSHA)); b != nil {
		t.Fatalf("a listed repo with no branches gave %v", b)
	}
	if ForgeOrigin(ForgeGitHub) != "https://api.github.com" || ForgeOrigin("gitlab") != "" {
		t.Fatal("forge origin wrong")
	}
}
