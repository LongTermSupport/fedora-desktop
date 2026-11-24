package semgrepreport

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

// A glob matches the way semgrep's paths.exclude does, which is the way a gitignore pattern
// does: at any depth unless it starts with /, * within one segment, ** across segments.
func TestExcludesMatchAsSemgrepDoes(t *testing.T) {
	excl, err := CompileExcludes([]string{"helpers/stdin_text/**", "/top.txt", "*.md", "CLAUDE/Plan/**/*.md", ".semgrep/x.*"})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"helpers/stdin_text/stdin_text.py":   true,
		"helpers/stdin_text/sub/deeper.py":   true,
		"x/helpers/stdin_text/stdin_text.py": true,
		"helpers/stdin_text_other/a.py":      false,
		"helpers/pingbus/cli.py":             false,
		"top.txt":                            true,
		"a/top.txt":                          false,
		"docs/readme.md":                     true,
		"CLAUDE/Plan/00001-a/PLAN.md":        true,
		"CLAUDE/Plan/00001-a/check.py":       false,
		".semgrep/x.yml":                     true,
		".semgrep/x/y.yml":                   false,
	} {
		if got := excl.Matches(path); got != want {
			t.Errorf("Matches(%q) = %v, want %v", path, got, want)
		}
	}
}

// No globs match nothing.
func TestNoExcludesMatchNothing(t *testing.T) {
	excl, err := CompileExcludes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if excl.Matches("a.py") {
		t.Fatal("an empty exclude list matched a file")
	}
}

// A rule's excludes are read from its YAML node; a rule with no paths has none, and a paths.exclude
// that is not a list of strings is refused rather than read as no excludes.
func TestRuleExcludesAreReadFromTheRuleNode(t *testing.T) {
	globs, err := RuleExcludes(ruleNode(t, "id: a\npaths:\n  exclude:\n    - 'a/**'\n    - \"b.py\"\n"))
	if err != nil || len(globs) != 2 || globs[0] != "a/**" || globs[1] != "b.py" {
		t.Fatalf("%q %v", globs, err)
	}
	for _, none := range []string{"id: a\n", "id: a\npaths:\n  include: ['x']\n"} {
		if globs, err := RuleExcludes(ruleNode(t, none)); err != nil || len(globs) != 0 {
			t.Fatalf("%q: %q %v", none, globs, err)
		}
	}
	for _, bad := range []string{
		"id: a\npaths:\n  exclude: a/**\n", "id: a\npaths:\n  exclude:\n    k: v\n",
		"id: a\npaths:\n  exclude:\n    - [x]\n", "id: a\npaths: 3\n", "- a\n",
	} {
		if _, err := RuleExcludes(ruleNode(t, bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// A rule file reads a path when one of its rules reads it: a file one rule excludes is still read
// by a sibling that does not.
func TestScopeReadsWhatSomeRuleReads(t *testing.T) {
	scope, err := ParseScope("r.yml", []byte(`rules:
  - id: a
    paths:
      exclude: &shared ['docs/**', 'CLAUDE/Plan/**/*.md']
  - id: b
    paths:
      exclude: ['notes/**', 'docs/**']
`))
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{
		"docs/a.md": false, "CLAUDE/Plan/00001-x/PLAN.md": true, "notes/a.md": true,
		"src/a.go": true, "CLAUDE/Plan/00001-x/run.bash": true,
	} {
		if got := scope.Reads(rel); got != want {
			t.Errorf("Reads(%s) = %v, want %v", rel, got, want)
		}
	}
}

// A rule with any paths.include reads every file, whatever its lists say: no real-semgrep test
// proves include matching, and a bare (`docs`) or trailing-slash (`docs/`) directory pattern is one
// the glob matcher would not match against `docs/a.md`, where semgrep would. Judging such a file
// "not read" would drop a gate that should run, so the error is made in the safe direction.
func TestScopeOfARuleWithAnIncludeReadsEverything(t *testing.T) {
	for _, include := range []string{"['docs']", "['docs/']", "['src/**']"} {
		scope, err := ParseScope("r.yml", []byte("rules:\n  - id: a\n    paths:\n      include: "+include+"\n      exclude: ['docs/b.md']\n"))
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"docs/a.md", "docs/b.md", "x.md", "deep/docs/c.md"} {
			if !scope.Reads(rel) {
				t.Errorf("include %s: Reads(%s) = false, want true", include, rel)
			}
		}
	}
}

func TestScopeOfARuleWithNoPathsReadsEverything(t *testing.T) {
	scope, err := ParseScope("r.yml", []byte("rules:\n  - id: a\n    paths:\n      exclude: ['docs/**']\n  - id: b\n    pattern: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !scope.Reads("docs/a.md") {
		t.Error("a rule with no paths reads every file, yet docs/a.md is not read")
	}
}

// A rule file that does not decode, or holds no rule, is refused: an empty scope would read
// nothing and every file would look excluded.
func TestScopeRefusesWhatItCannotRead(t *testing.T) {
	for name, text := range map[string]string{
		"not yaml":     "rules: [",
		"no rules":     "rules: []\n",
		"bad exclude":  "rules:\n  - id: a\n    paths:\n      exclude: x\n",
		"no rules key": "other: 1\n",
	} {
		if _, err := ParseScope("r.yml", []byte(text)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// ruleNode is the YAML text decoded as one node, as a rule file's rules list holds each rule.
func ruleNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}
