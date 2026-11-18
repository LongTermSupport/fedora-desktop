package pushgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// globCase is one path and whether the pattern matches it.
type globCase struct {
	pattern string
	path    string
	want    bool
}

// GitHub's "Filter pattern cheat sheet" for paths, each example as given there, and the paths
// each pattern must not match.
func TestTheGlobMatchesGitHubsCheatSheet(t *testing.T) {
	for _, tc := range []globCase{
		{"*", "README.md", true},
		{"*", "server.rb", true},
		{"*", "docs/README.md", false},
		{"*.jsx?", "page.js", true},
		{"*.jsx?", "page.jsx", true},
		{"*.jsx?", "page.jsxx", false},
		{"**", "all/the/files.md", true},
		{"*.js", "app.js", true},
		{"*.js", "index.js", true},
		{"*.js", "js/index.js", false},
		{"**.js", "index.js", true},
		{"**.js", "js/index.js", true},
		{"**.js", "src/js/app.js", true},
		{"docs/*", "docs/README.md", true},
		{"docs/*", "docs/file.txt", true},
		{"docs/*", "docs/mona/octocat.txt", false},
		{"docs/**", "docs/README.md", true},
		{"docs/**", "docs/mona/octocat.txt", true},
		{"docs/**", "mydocs/x.md", false},
		{"docs/**/*.md", "docs/README.md", true},
		{"docs/**/*.md", "docs/mona/hello-world.md", true},
		{"docs/**/*.md", "docs/a/markdown/file.md", true},
		{"docs/**/*.md", "docs/a/file.txt", false},
		{"**/docs/**", "docs/hello.md", true},
		{"**/docs/**", "dir/docs/my-file.txt", true},
		{"**/docs/**", "space/docs/plan/space.doc", true},
		{"**/docs/**", "mydocs/x.txt", false},
		{"**/README.md", "README.md", true},
		{"**/README.md", "js/README.md", true},
		{"**/README.md", "js/NOTREADME.md", false},
		{"**/*src/**", "a/src/app.js", true},
		{"**/*src/**", "my-src/code/js/app.js", true},
		{"**/*-post.md", "my-post.md", true},
		{"**/*-post.md", "path/their-post.md", true},
		{"**/migrate-*.sql", "migrate-10909.sql", true},
		{"**/migrate-*.sql", "db/migrate-v1.0.sql", true},
		{"**/migrate-*.sql", "db/sept/migrate-v1.sql", true},
		{"a+b.txt", "ab.txt", true},
		{"a+b.txt", "aaab.txt", true},
		{"a+b.txt", "b.txt", false},
		{"file[0-9].txt", "file7.txt", true},
		{"file[0-9].txt", "filex.txt", false},
		{"file[abc].txt", "fileb.txt", true},
		{`a\*b`, "a*b", true},
		{`a\*b`, "axb", false},
		{"a.b", "axb", false},
	} {
		re, err := compileGlob(tc.pattern)
		if err != nil {
			t.Errorf("%q: %v", tc.pattern, err)
			continue
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("%q matching %q = %t, want %t", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// A pattern the matcher cannot read the way GitHub does is refused, never guessed.
func TestAGlobOutsideTheSupportedSyntaxIsRefused(t *testing.T) {
	for _, p := range []string{"", "/docs/**", "./docs/**", "***", "?a", "+a", "*?", "a??", "file[].txt", "file[a-.].txt", "file[z-a].txt", "file[a-Z].txt", "file[0-9", "a]b", `a\`, "a!b", "[!a]"} {
		if _, err := compileGlob(p); err == nil {
			t.Errorf("%q compiled; want it refused", p)
		}
	}
}

// filterCase is one path filter and the files GitHub would start the workflow for.
type filterCase struct {
	name    string
	trigger string
	runs    map[string]bool
}

// The cheat sheet's examples of paths with ! patterns, and paths-ignore.
func TestAFilterIsReadTheWayGitHubReadsIt(t *testing.T) {
	for _, tc := range []filterCase{
		{"paths, the last match decides", "paths: ['*.md', '!README.md']", map[string]bool{
			"hello.md": true, "README.md": false, "docs/hello.md": false,
		}},
		{"paths, a later pattern takes back a !", "paths: ['*.md', '!README.md', 'README*']", map[string]bool{
			"hello.md": true, "README.md": true, "README.doc": true, "x.txt": false,
		}},
		{"paths-ignore, any match leaves a file out", "paths-ignore: ['docs/**', '**.md']", map[string]bool{
			"docs/x.png": false, "a/b.md": false, "main.go": true,
		}},
		{"no filter runs for every file", "branches: [F44]", map[string]bool{"README.md": true}},
		{"a null push runs for every file", "", map[string]bool{"README.md": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parsePushFilter([]byte(workflowWithPush(tc.trigger)))
			if err != nil {
				t.Fatal(err)
			}
			for file, want := range tc.runs {
				if got := f.runsFor(file); got != want {
					t.Errorf("%s: runsFor = %t, want %t", file, got, want)
				}
			}
		})
	}
}

// workflowWithPush is a workflow whose push trigger holds these lines (YAML flow style).
func workflowWithPush(lines string) string {
	body := "name: QA\non:\n  push:\n"
	for line := range strings.SplitSeq(lines, "\n") {
		if line != "" {
			body += "    " + line + "\n"
		}
	}
	return body + "  pull_request:\njobs: {}\n"
}

func TestTheShortFormsOfOnRunForEveryFile(t *testing.T) {
	for _, wf := range []string{"on: push\n", "on: [push, pull_request]\n"} {
		f, err := parsePushFilter([]byte(wf))
		if err != nil {
			t.Fatalf("%q: %v", wf, err)
		}
		if !f.runsFor("README.md") {
			t.Errorf("%q: a workflow with no path filter must run for documentation", wf)
		}
	}
}

// A workflow whose push filter cannot be read is an error, so the gate requires CI's run.
func TestAPushFilterThatCannotBeReadIsAnError(t *testing.T) {
	for name, wf := range map[string]string{
		"not YAML":            "on: [push\n",
		"no on":               "name: QA\n",
		"no push":             "on:\n  pull_request:\n",
		"on names no push":    "on: pull_request\n",
		"both filters":        workflowWithPush("paths: ['**']\npaths-ignore: ['docs/**']"),
		"paths not a list":    workflowWithPush("paths: '**'"),
		"an empty list":       workflowWithPush("paths: []"),
		"a non-string item":   workflowWithPush("paths: [[a]]"),
		"only negations":      workflowWithPush("paths: ['!docs/**']"),
		"a negated ignore":    workflowWithPush("paths-ignore: ['!docs/**']"),
		"an unsupported glob": workflowWithPush("paths: ['{a,b}/**', '[!a]']"),
		"push not a mapping":  "on:\n  push: [a]\n",
		"a repeated on":       "on: push\non: push\n",
		"a repeated push":     "on:\n  push:\n  push:\n",
		"a repeated paths":    workflowWithPush("paths: ['**']\npaths: ['**']"),
		"the document a list": "- on\n",
	} {
		if _, err := parsePushFilter([]byte(wf)); err == nil {
			t.Errorf("%s: parsed; want an error", name)
		}
	}
}

// The filter qa.yml carries, read from the file itself: documentation is left out, and the
// Markdown that is code is kept in.
func TestQAWorkflowsFilterLeavesOutOnlyDocumentation(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, QAWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	f, err := parsePushFilter(data)
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		"README.md":                          false,
		"docs/guide/install.md":              false,
		"docs/x.png":                         false,
		"notes/design.md":                    false,
		"CLAUDE/QA.md":                       false,
		"CLAUDE/Plan/00001-x/JOURNAL/a.md":   false,
		"CLAUDE/Plan/00001-x/PLAN.md":        false,
		"CLAUDE.md":                          true,
		"src-go/CLAUDE.md":                   true,
		"playbooks/dev/CLAUDE.md":            true,
		"AGENTS.md":                          true,
		"files/foo.md":                       true,
		"files/opt/x/skills/y/SKILL.md":      true,
		"extensions/a/README.md":             true,
		".claude/rules/go-code.md":           true,
		".github/PULL_REQUEST_TEMPLATE.md":   true,
		"CHANGELOG.md":                       true,
		"src-go/vendor/charm.land/README.md": true,
		"src-go/main.go":                     true,
		"scripts/qa-all.bash":                true,
	} {
		if got := f.runsFor(file); got != want {
			t.Errorf("%s: qa.yml runs = %t, want %t", file, got, want)
		}
	}
}
