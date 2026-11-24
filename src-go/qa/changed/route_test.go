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
post	any:@file	gitleaks	-
perfile-gate	qa-bash qa-python	qa-go
`

func TestLoadRoutingParsesEveryColumn(t *testing.T) {
	path := writeFile(t, t.TempDir(), "r.tsv", goodRouting)
	r, err := loadRouting(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.also) != 4 || len(r.first) != 5 || len(r.post) != 3 {
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
	if !reflect.DeepEqual(r.post[2].anyOf, []string{"@file"}) {
		t.Fatalf("post: %+v", r.post[2])
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

// A comments row says what a file's change to its comments and blank lines alone runs; only a
// file qa-changed can compare by its rows may have one (issue #413).
func TestLoadRoutingReadsACommentsRowForAFileItCanCompare(t *testing.T) {
	path := writeFile(t, t.TempDir(), "r.tsv", "comments\tscripts/qa-trees.tsv\tgo\tcovered\tqa/ghscopes,qa/bashlimits\n")
	r, err := loadRouting(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.comments) != 1 || len(r.also) != 0 || len(r.first) != 0 {
		t.Fatalf("rule counts: comments %d also %d first %d", len(r.comments), len(r.also), len(r.first))
	}
	if c := r.comments[0]; !c.covered || !reflect.DeepEqual(c.goPackages, []string{"qa/ghscopes", "qa/bashlimits"}) {
		t.Fatalf("comments row: %+v", c)
	}
	for name, body := range map[string]string{
		"a file it cannot compare": "comments\tscripts/qa-gates.tsv\tgo\tcovered\t@full\n",
		"a glob":                   "comments\tscripts/*.tsv\tgo\tcovered\t@full\n",
		"a predicate":              "comments\t@sh\tbash-limits\t-\n",
		"@yaml":                    "comments\tscripts/qa-trees.tsv\t@yaml\t-\n",
	} {
		path := writeFile(t, t.TempDir(), "r.tsv", "# ok\n"+body)
		if _, err := loadRouting(path); err == nil || !strings.Contains(err.Error(), ":2:") {
			t.Errorf("%s: want an error naming line 2, got %v", name, err)
		}
	}
}

// go-qa's diff mode sees only src-go/, so a row that routes a file outside it to the go check
// names the Go packages that read the file, or @full for a file that can change every result.
func TestLoadRoutingReadsTheGoPackagesColumn(t *testing.T) {
	body := "also\tfiles/x/*.bash\tgo\t-\tinternal/a,internal/b/...\n" +
		"also\tscripts/qa-routing.tsv\tbash-limits,go\tcovered\t@full\n" +
		"first\tsrc-go/*\tgo\tcovered,closure\n"
	r, err := loadRouting(writeFile(t, t.TempDir(), "r.tsv", body))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.also[0].goPackages, []string{"internal/a", "internal/b/..."}) || r.also[0].goFull {
		t.Fatalf("packages row: %+v", r.also[0])
	}
	if !r.also[1].goFull || len(r.also[1].goPackages) != 0 || !r.also[1].covered {
		t.Fatalf("@full row: %+v", r.also[1])
	}
	if r.first[0].goFull || len(r.first[0].goPackages) != 0 {
		t.Fatalf("src-go row: %+v", r.first[0])
	}
}

func TestLoadRoutingRefusesABadGoPackagesColumn(t *testing.T) {
	cases := map[string]string{
		"outside file, no column":       "also\tfiles/x\tgo\t-\n",
		"partly outside, no column":     "also\tsrc-go/a,files/x\tgo\t-\n",
		"predicate, no column":          "also\t@py\tgo\t-\n",
		"column without go":             "also\tfiles/x\tbash-limits\t-\tinternal/a\n",
		"column on a src-go row":        "also\tsrc-go/a/*\tgo\t-\tinternal/a\n",
		"empty column":                  "also\tfiles/x\tgo\t-\t\n",
		"empty entry":                   "also\tfiles/x\tgo\t-\tinternal/a,\n",
		"dash":                          "also\tfiles/x\tgo\t-\t-\n",
		"src-go prefix":                 "also\tfiles/x\tgo\t-\tsrc-go/internal/a\n",
		"dot slash":                     "also\tfiles/x\tgo\t-\t./internal/a\n",
		"absolute":                      "also\tfiles/x\tgo\t-\t/internal/a\n",
		"parent":                        "also\tfiles/x\tgo\t-\t../internal/a\n",
		"unclean":                       "also\tfiles/x\tgo\t-\tinternal//a\n",
		"full with packages":            "also\tfiles/x\tgo\t-\t@full,internal/a\n",
		"six columns":                   "also\tfiles/x\tgo\t-\tinternal/a\textra\n",
		"unknown at-word":               "also\tfiles/x\tgo\t-\t@all\n",
		"repeated package":              "also\tfiles/x\tgo\t-\tinternal/a,internal/a\n",
		"bare tree":                     "also\tfiles/x\tgo\t-\t...\n",
		"dots inside":                   "also\tfiles/x\tgo\t-\tinternal/.../a\n",
		"post row with a fifth column":  "post\tany:@sh\tgo\t-\tinternal/a\n",
		"yaml first row with go column": "first\t*.yml\t@yaml\t-\tinternal/a\n",
	}
	for name, body := range cases {
		path := writeFile(t, t.TempDir(), "r.tsv", "# ok\n"+body)
		if _, err := loadRouting(path); err == nil || !strings.Contains(err.Error(), ":2:") {
			t.Errorf("%s: want an error naming line 2, got %v", name, err)
		}
	}
}

// A check planned by two rules with different rel would keep the first rel silently
// (plan.add), so the table may not say two things about one check.
func TestLoadRoutingRefusesOneCheckWithTwoRels(t *testing.T) {
	for name, body := range map[string]string{
		"two rels":       "also\ta/*.js\teslint\trel:a\nalso\tb/*.js\teslint\trel:b\n",
		"rel and no rel": "also\ta/*.js\teslint\trel:a\nfirst\t*.js\teslint\t-\n",
	} {
		_, err := loadRouting(writeFile(t, t.TempDir(), "r.tsv", body))
		if err == nil || !strings.Contains(err.Error(), "eslint") || !strings.Contains(err.Error(), ":2:") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	same := "also\ta/*.js\teslint\trel:a\nfirst\tb/*.js\teslint\trel:a\n"
	if _, err := loadRouting(writeFile(t, t.TempDir(), "same.tsv", same)); err != nil {
		t.Fatalf("one rel twice is fine: %v", err)
	}
}

// A sweep that judges files of one kind is a test module the kind's row names: unit-tests:<module>
// runs that module for any file the row matches, whatever the file is.
func TestLoadRoutingReadsNamedTestModules(t *testing.T) {
	body := "also\tCLAUDE/Plan/*.bash\tplan-tests,unit-tests:tests.helpers.a.test_b,unit-tests:tests.helpers.test_c\t-\n" +
		"first\t@py\truff,unit-tests\tcovered\n"
	r, err := loadRouting(writeFile(t, t.TempDir(), "r.tsv", body))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.also[0].checks, []string{"plan-tests"}) {
		t.Fatalf("checks: %v", r.also[0].checks)
	}
	if !reflect.DeepEqual(r.also[0].modules, []string{"tests.helpers.a.test_b", "tests.helpers.test_c"}) {
		t.Fatalf("modules: %v", r.also[0].modules)
	}
	if len(r.first[0].modules) != 0 || !reflect.DeepEqual(r.first[0].checks, []string{"ruff", "unit-tests"}) {
		t.Fatalf("a plain unit-tests names no module: %+v", r.first[0])
	}
}

func TestLoadRoutingRefusesABadNamedTestModule(t *testing.T) {
	cases := map[string]string{
		"empty module":        "also\t*.bash\tunit-tests:\t-\n",
		"not under tests":     "also\t*.bash\tunit-tests:helpers.a.test_b\t-\n",
		"not a test module":   "also\t*.bash\tunit-tests:tests.helpers.a.b\t-\n",
		"empty segment":       "also\t*.bash\tunit-tests:tests..test_b\t-\n",
		"path not module":     "also\t*.bash\tunit-tests:tests/helpers/test_b.py\t-\n",
		"segment not an id":   "also\t*.bash\tunit-tests:tests.1x.test_b\t-\n",
		"module twice":        "also\t*.bash\tunit-tests:tests.test_b,unit-tests:tests.test_b\t-\n",
		"other check, module": "also\t*.bash\tbash-limits:tests.test_b\t-\n",
		"post row module":     "post\tany:@sh\tunit-tests:tests.test_b\t-\n",
		"rel on module row":   "also\t*.bash\tunit-tests:tests.test_b\trel:x\n",
	}
	for name, body := range cases {
		path := writeFile(t, t.TempDir(), "r.tsv", "# ok\n"+body)
		if _, err := loadRouting(path); err == nil || !strings.Contains(err.Error(), ":2:") {
			t.Errorf("%s: want an error naming line 2, got %v", name, err)
		}
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
