package semgrepreport

import (
	"strings"
	"testing"
)

// excludeRule is a rule file shaped like the real ones; DSTAR and STAR stand for the glob stars.
var excludeRule = strings.NewReplacer("DSTAR", "**", "STAR", "*").Replace(`rules:
  - id: one
    paths:
      exclude:
        - 'vars/github-required-scopes.yml'
        - 'helpers/github_scopes/DSTAR'
        - '.semgrep/gh-scopes.STAR'
        - 'CLAUDE/Plan/DSTAR/STAR.md'
        - '/rooted.txt'
        - 'a?c'
  - id: two
    paths:
      exclude:
        - "STAR_test.go"
`)

func TestExcludesMatchAsGitignorePatterns(t *testing.T) {
	ex, err := ParseExcludes("r.yml", excludeRule)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"vars/github-required-scopes.yml":     true,
		"sub/vars/github-required-scopes.yml": true,
		"helpers/github_scopes/x/y.py":        true,
		".semgrep/gh-scopes.yml":              true,
		".semgrep/gh-scopes/deep":             false,
		"CLAUDE/Plan/PLAN.md":                 true,
		"CLAUDE/Plan/a/b/PLAN.md":             true,
		"rooted.txt":                          true,
		"sub/rooted.txt":                      false,
		"abc":                                 true,
		"a/c":                                 false,
		"src/x_test.go":                       true,
		"src/x.go":                            false,
	} {
		if got := ex.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExcludesRefuseARuleFileWithoutThem(t *testing.T) {
	for name, text := range map[string]string{
		"no list":  "rules:\n  - id: x\n",
		"not yaml": "rules: [\n",
	} {
		if _, err := ParseExcludes("r.yml", text); err == nil || !strings.Contains(err.Error(), "r.yml") {
			t.Errorf("%s: err %v, want one naming r.yml", name, err)
		}
	}
}
