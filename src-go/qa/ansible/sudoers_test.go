package ansible

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// sudoersFixtureDir holds two small repository trees, flagged/ and clean/: the rule reads a
// play's sudoers source and the tasks that deploy its targets, so each case is a tree.
var sudoersFixtureDir = filepath.Join("..", "..", "..", "tests", "fixtures", "ansible-sudoers")

func expectedFindingLines(t *testing.T, path string) []int {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want []int
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \r"), "# EXPECT-FINDING") {
			want = append(want, i+1)
		}
	}
	return want
}

func TestSudoersRuleProvesItselfAgainstTheFlaggedFixture(t *testing.T) {
	root := filepath.Join(sudoersFixtureDir, "flagged")
	want := expectedFindingLines(t, filepath.Join(root, "tasks", "main.yml"))
	if len(want) < 25 {
		t.Fatalf("the flagged fixture marks only %d findings; it lost cases", len(want))
	}
	findings, err := sudoersRule(os.DirFS(root), []string{"tasks"})
	if err != nil {
		t.Fatal(err)
	}
	if got := findingLines(t, "tasks/main.yml", findings); !slices.Equal(got, want) {
		t.Fatalf("finding lines\n got %v\nwant %v\n%s", got, want, strings.Join(findings, "\n"))
	}
}

func TestSudoersRuleIsSilentOnTheCleanFixture(t *testing.T) {
	findings, err := sudoersRule(os.DirFS(filepath.Join(sudoersFixtureDir, "clean")), []string{"tasks"})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean fixture reported:\n%s", strings.Join(findings, "\n"))
	}
}

// sudoersTree is a tree with one task list that installs the rule text, and a root-owned
// deploy of /usr/local/bin/tool, which every rule below names.
func sudoersTree(rule string) fstest.MapFS {
	return fstest.MapFS{
		"tasks/main.yml": {Data: []byte(`- ansible.builtin.copy:
    src: "{{ root_dir }}/files/tool"
    dest: /usr/local/bin/tool
    owner: root
    group: root
    mode: "0755"
- ansible.builtin.template:
    src: "{{ root_dir }}/files/rule.j2"
    dest: /etc/sudoers.d/rule
    owner: root
    group: root
    mode: "0440"
    validate: visudo -cf %s
`)},
		"files/rule.j2": {Data: []byte(rule)},
		"files/tool":    {Data: []byte("#!/bin/bash\n")},
	}
}

func TestSudoersRuleJudgesEverySpellingOfTheRuleText(t *testing.T) {
	cases := []struct {
		name, rule, want string
	}{
		{"pinned arguments", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool run, /usr/local/bin/tool \"\"\n", ""},
		{"the user variable without spaces", "{{user_login}} ALL=(root) NOPASSWD: /usr/local/bin/tool \"\"\n", ""},
		{"no runas is root", "{{ user_login }} ALL= NOPASSWD: /usr/local/bin/tool \"\"\n", ""},
		{"a continuation joins the list", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool \"\", \\\n    /usr/local/bin/tool\n", "admits any arguments"},
		{"SETENV after NOPASSWD", "{{ user_login }} ALL=(root) NOPASSWD:SETENV: /usr/local/bin/tool \"\"\n", "SETENV"},
		{"env_keep for the user", "Defaults:{{ user_login }} env_keep += \"PATH\"\n", "env_keep"},
		{"setenv for the command", "Defaults!/usr/local/bin/tool setenv\n", "setenv"},
		{"secure_path changed", "Defaults secure_path = /home/u/bin:/usr/bin\n", "secure_path"},
		{"env_reset off", "Defaults !env_reset\n", "env_reset"},
		{"an include", "@includedir /etc/sudoers.d/more\n", "include"},
		{"a user alias", "User_Alias ADMINS = a, b\n", "alias"},
		{"the user is ALL", "ALL ALL=(root) NOPASSWD: /usr/local/bin/tool \"\"\n", "names no fixed user"},
		{"the user is another variable", "{{ other_user }} ALL=(root) NOPASSWD: /usr/local/bin/tool \"\"\n", "names no fixed user"},
		{"runas a list with ALL", "{{ user_login }} ALL=(root, ALL) NOPASSWD: /usr/local/bin/tool \"\"\n", "runas"},
		{"a glob in the path", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/to?l \"\"\n", "wildcard"},
		{"a relative path", "{{ user_login }} ALL=(root) NOPASSWD: tool \"\"\n", "absolute"},
		{"a regex anchored at one end", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool ^run [a-z]*\n", "anchored"},
		{"a regex with a bare dot", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool ^run [a-z]+.$\n", "any character"},
		{"an escaped dot is a dot", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool ^run [a-z]+\\.conf$\n", ""},
		{"a dot in a bracket is a dot", "{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool ^run [a-z.]+$\n", ""},
		{"sudoedit", "{{ user_login }} ALL=(root) NOPASSWD: sudoedit /etc/hosts\n", "sudoedit"},
		{"a full grant is not a scoped rule", "{{ user_login }} ALL=(ALL) NOPASSWD: ALL\n", ""},
		{"a comment is not read", "# {{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool\n", ""},
		{"Defaults that tighten", "Defaults!/usr/local/bin/tool !setenv, env_reset\n", ""},
	}
	for _, c := range cases {
		findings, err := sudoersRule(sudoersTree(c.rule), []string{"tasks"})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		joined := strings.Join(findings, "\n")
		switch {
		case c.want == "" && len(findings) != 0:
			t.Errorf("%s: want none, got\n%s", c.name, joined)
		case c.want != "" && (len(findings) != 1 || !strings.Contains(joined, c.want)):
			t.Errorf("%s: want one finding naming %q, got\n%s", c.name, c.want, joined)
		}
	}
}

func TestSudoersRuleNamesTheRuleLineAndTheFix(t *testing.T) {
	findings, err := sudoersRule(sudoersTree("# a comment\n{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/tool\n"), []string{"tasks"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tasks/main.yml:7: files/rule.j2 line 2: /usr/local/bin/tool admits any arguments; " +
		"pin each argument list the target accepts (\"\" for none, a literal, or an anchored ^...$ expression)"}
	if !slices.Equal(findings, want) {
		t.Fatalf("got %q", findings)
	}
}

func TestSudoersRuleIsReportedByTheGate(t *testing.T) {
	tree := sudoersTree("{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/ghost \"\"\n")
	tree["playbooks/p.yml"] = &fstest.MapFile{Data: []byte("- hosts: all\n  tasks: []\n")}
	var f Findings
	if err := scanSudoers(tree, []string{"playbooks", "tasks"}, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Sudoers) != 1 || f.Errors() != 1 || !slices.Contains(f.Out, sudoersAdvice) ||
		!strings.Contains(f.Out[0], "  ERROR (sudoers-rule): tasks/main.yml:7: ") {
		t.Fatalf("%+v", f)
	}
}
