package changed

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const goodRouting = `# comment
also	*.bash,*.sh	bash-limits	-
also	scripts/bash-limits.allowlist	bash-limits	covered
also	@listed:scripts/bash-limits.allowlist	bash-limits	-
also	extensions/` + star + `.js	eslint	tree:shell,rel:extensions

first	*.md	docs	covered
first	src-go/` + star + `	go	covered,closure
first	*.yml,*.yaml	@yaml	-
first	@py	ruff,unit-tests	covered,tree:python
first	@sh	bash-syntax,shellcheck,semgrep	covered,tree:shell
post	any:@sh	bash-limits	-
post	any:@sh,any:@py	toolchain	-
perfile-gate	qa-bash qa-python	qa-go
`

func TestLoadRoutingParsesEveryColumn(t *testing.T) {
	path := writeFile(t, t.TempDir(), "r.tsv", goodRouting)
	r, err := loadRouting(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.also) != 4 || len(r.first) != 5 || len(r.post) != 2 {
		t.Fatalf("rule counts: %d %d %d", len(r.also), len(r.first), len(r.post))
	}
	if !reflect.DeepEqual(r.perFileGates, []string{"qa-bash", "qa-python", "qa-go"}) {
		t.Fatalf("gates: %v", r.perFileGates)
	}
	checkParsedRules(t, r)
}

func checkParsedRules(t *testing.T, r *routing) {
	t.Helper()
	eslint := r.also[3]
	if eslint.tree != "shell" || eslint.rel != "extensions" || eslint.covered {
		t.Fatalf("flags: %+v", eslint)
	}
	if !r.also[1].covered || r.also[2].m.listed != "scripts/bash-limits.allowlist" {
		t.Fatalf("also rules: %+v %+v", r.also[1], r.also[2])
	}
	goRule := r.first[1]
	if !goRule.closure || !goRule.covered || goRule.checks[0] != "go" {
		t.Fatalf("go rule: %+v", goRule)
	}
	if !r.first[2].yaml || len(r.first[2].checks) != 0 {
		t.Fatalf("yaml rule: %+v", r.first[2])
	}
	if r.first[3].m.kind != "@py" || r.first[4].m.kind != "@sh" {
		t.Fatalf("predicates: %+v %+v", r.first[3].m, r.first[4].m)
	}
	if !reflect.DeepEqual(r.post[1].anyOf, []string{"@sh", "@py"}) {
		t.Fatalf("post: %+v", r.post[1])
	}
}

func TestLoadRoutingRefusesBadRowsWithTheLineNumber(t *testing.T) {
	cases := map[string]string{
		"short row":         "also\t*.bash\tbash-limits\n",
		"extra column":      "also\t*.bash\tbash-limits\t-\textra\n",
		"unknown phase":     "later\t*.bash\tbash-limits\t-\n",
		"unknown check":     "also\t*.bash\tnot-a-check\t-\n",
		"unknown flag":      "also\t*.bash\tbash-limits\tcovred\n",
		"unknown tree":      "first\t*.js\tnode-check\ttree:nowhere\n",
		"unknown predicate": "first\t@rust\tgo\t-\n",
		"yaml in also":      "also\t*.yml\t@yaml\t-\n",
		"bad post":          "post\tsome:@sh\tgo\t-\n",
		"empty gate row":    "perfile-gate\n",
		"empty listed":      "also\t@listed:\tgo\t-\n",
		"empty rel":         "also\t*.js\teslint\trel:\n",
	}
	for name, body := range cases {
		path := writeFile(t, t.TempDir(), "r.tsv", "# ok\n"+body)
		_, err := loadRouting(path)
		if err == nil || !strings.Contains(err.Error(), ":2:") {
			t.Errorf("%s: want an error naming line 2, got %v", name, err)
		}
	}
	if _, err := loadRouting(filepath.Join(t.TempDir(), "missing.tsv")); err == nil {
		t.Fatal("a missing routing table must be an error")
	}
}

func TestLoadTreesReadsBothLists(t *testing.T) {
	path := writeFile(t, t.TempDir(), "t.tsv", "# c\nshell\t.git\nshell\tuntracked\npython\t.git\npython\t__pycache__\n")
	trees, err := loadTrees(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trees.shell, treeList{".git", "untracked"}) || !reflect.DeepEqual(trees.python, treeList{".git", "__pycache__"}) {
		t.Fatalf("got %+v", trees)
	}
	for _, bad := range []string{"shell\n", "ruby\tx\n", "shell\ta\tb\n", "shell\t\n"} {
		if _, err := loadTrees(writeFile(t, t.TempDir(), "t.tsv", bad)); err == nil || !strings.Contains(err.Error(), ":1:") {
			t.Errorf("%q: want an error naming line 1, got %v", bad, err)
		}
	}
	if _, err := loadTrees(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing trees table must be an error")
	}
	if got := trees.byName("python"); !reflect.DeepEqual(got, trees.python) {
		t.Fatalf("byName: %v", got)
	}
}

func TestMatcherMatches(t *testing.T) {
	probe := fileProbe{root: t.TempDir()}
	writeFile(t, probe.root, "tool", "#!/usr/bin/bash\n")
	writeFile(t, probe.root, "pytool", "#!/usr/bin/python3\n")
	listed := map[string]bool{"scripts/x": true}
	mk := func(tab string) matcher {
		m, err := parseMatcher(tab)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	for _, c := range []struct {
		m    string
		rel  string
		want bool
	}{
		{"*.md,*.txt", "a/b.txt", true},
		{"*.md,*.txt", "a/b.go", false},
		{"@sh", "tool", true},
		{"@sh", "pytool", false},
		{"@py", "pytool", true},
		{"@py", "x.py", true},
		{"@listed:f", "scripts/x", true},
		{"@listed:f", "scripts/y", false},
	} {
		got, err := mk(c.m).matches(c.rel, probe, listed)
		if err != nil || got != c.want {
			t.Errorf("%s on %s: got %v, %v want %v", c.m, c.rel, got, err, c.want)
		}
	}
	if _, err := mk("@sh").matches("missing", probe, listed); err == nil {
		t.Fatal("an unreadable file must stop the match")
	}
	if _, err := mk("@py").matches("missing", probe, listed); err == nil {
		t.Fatal("an unreadable file must stop the match")
	}
	if mk("@sh").contentBased() != true || mk("*.md").contentBased() || mk("@listed:f").contentBased() {
		t.Fatal("contentBased: only the shebang predicates read the file")
	}
}
