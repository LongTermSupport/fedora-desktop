package changed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobMatchHasBashCaseSemantics(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*.bash", "a.bash", true},
		{"*.bash", "scripts/deep/a.bash", true}, // the star crosses a slash
		{"*.bash", "a.bash.txt", false},
		{"src-go/" + star, "src-go/x/y/z.go", true},
		{"src-go/" + star, "src-go", false},
		{"src-go/" + star, "xsrc-go/a", false},
		{"helpers/" + star + ".py", "helpers/a/b/c.py", true},
		{"tests/helpers/" + star + "/test_" + star + ".py", "tests/helpers/gnome/test_x.py", true},
		{"tests/helpers/" + star + "/test_" + star + ".py", "tests/helpers/test_x.py", false},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "acb", false},
		{"exact.md", "exact.md", true},
		{"exact.md", "exact.mdx", false},
		{"*", "", true},
		{"", "", true},
		{"", "a", false},
		{"a*", "a", true},
		{"*a", "ba", true},
		{"*a*", "b", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestTreesExcludeAtAnyDepthAsWholeDirectoryNames(t *testing.T) {
	trees := treeList{"untracked", ".claude/worktrees", "node_modules"}
	for path, want := range map[string]bool{
		"untracked/x.bash":               true,
		"a/untracked/x.bash":             true,
		".claude/worktrees/w/x.bash":     true,
		"extensions/node_modules/m/x.js": true,
		"untrackedx/x.bash":              false,
		"x/untracked.bash":               false,
		".claude/worktreesx/a":           false,
		"scripts/qa-bash.bash":           false,
		"notuntracked/a.bash":            false,
		"untracked":                      true,
	} {
		if got := trees.excludes(path); got != want {
			t.Errorf("excludes(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestShebangForms(t *testing.T) {
	shell := []string{"#!/usr/bin/bash", "#!/bin/bash", "#!/usr/bin/env bash", "#!/bin/sh", "#!/usr/bin/sh", "#!/usr/bin/bash -e"}
	notShell := []string{"#!/usr/bin/bash -c 'ansible-playbook x'", "#!/bin/dash", "#!/usr/bin/env python3", "", "bash", "#!/bin/sh -e"}
	for _, line := range shell {
		if !isShellShebang(line) {
			t.Errorf("%q should be a shell shebang", line)
		}
	}
	for _, line := range notShell {
		if isShellShebang(line) {
			t.Errorf("%q should not be a shell shebang", line)
		}
	}
	for line, want := range map[string]bool{
		"#!/usr/bin/python3":     true,
		"#!/usr/bin/env python3": true,
		"#!/usr/bin/python":      true,
		"#!/usr/bin/bash":        false,
		"# python":               false,
		"":                       false,
	} {
		if isPythonShebang(line) != want {
			t.Errorf("isPythonShebang(%q) != %v", line, want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if line, ok, err := firstLine(write("a", "#!/bin/sh\nmore\n")); err != nil || !ok || line != "#!/bin/sh" {
		t.Fatalf("got %q %v %v", line, ok, err)
	}
	if line, ok, err := firstLine(write("b", "#!/bin/sh")); err != nil || !ok || line != "#!/bin/sh" {
		t.Fatalf("no trailing newline: got %q %v %v", line, ok, err)
	}
	if _, ok, err := firstLine(write("c", "")); err != nil || ok {
		t.Fatalf("an empty file has no first line: %v %v", ok, err)
	}
	long := strings.Repeat("x", 20000)
	if line, ok, err := firstLine(write("d", long+"\n")); err != nil || !ok || line == "" {
		t.Fatalf("a long first line: %d %v %v", len(line), ok, err)
	}
	if _, _, err := firstLine(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("an unreadable file must be an error, not 'not a script'")
	}
}

func TestFileProbeReadsTheShebangOnlyWhenTheNameDoesNotSay(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/usr/bin/env python3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sh-tool"), []byte("#!/usr/bin/bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fileProbe{root: dir}
	if py, err := probe.isPython("tool"); err != nil || !py {
		t.Fatalf("python by shebang: %v %v", py, err)
	}
	if py, err := probe.isPython("x.py"); err != nil || !py {
		t.Fatalf("python by extension needs no read: %v %v", py, err)
	}
	if sh, err := probe.isShell("sh-tool"); err != nil || !sh {
		t.Fatalf("shell by shebang: %v %v", sh, err)
	}
	if sh, err := probe.isShell("x.bash"); err != nil || !sh {
		t.Fatalf("shell by extension: %v %v", sh, err)
	}
	if sh, err := probe.isShell("tool"); err != nil || sh {
		t.Fatalf("a python tool is not shell: %v %v", sh, err)
	}
	if _, err := probe.isShell("missing"); err == nil {
		t.Fatal("an unreadable file must stop classification")
	}
	if _, err := probe.isPython("missing"); err == nil {
		t.Fatal("an unreadable file must stop classification")
	}
}
