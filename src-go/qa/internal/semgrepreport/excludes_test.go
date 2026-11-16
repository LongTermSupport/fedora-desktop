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

// ruleNode is the YAML text decoded as one node, as a rule file's rules list holds each rule.
func ruleNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}
