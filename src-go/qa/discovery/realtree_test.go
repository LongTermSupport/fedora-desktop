package discovery

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// These tests run the real git and the real wrapper, scripts/qa-discovery.bash, over a real work
// tree: what the fake git of the other tests cannot show.

// repoScripts is the repository's scripts directory (the tests run in src-go/qa/discovery).
func repoScripts(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "qa-discovery.bash")); err != nil {
		t.Fatalf("the repository's scripts directory is not at %s: %v", dir, err)
	}
	return dir
}

// gitTree makes a real git work tree: tracked files, an ignored one, an untracked one.
func gitTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: root})
		if err != nil || res.Code != 0 {
			t.Fatalf("git %v: %v %+v", args, err, res)
		}
	}
	git("init", "-q")
	for rel, text := range map[string]string{
		"a.bash":                   "echo a\n",
		"tool":                     "#!/usr/bin/env bash\n",
		"py/b.py":                  "print(1)\n",
		"playbooks/p.yml":          "- hosts: all\n",
		"tests/helpers/test_h.py":  "x = 1\n",
		"files/t.j2":               "#!/usr/bin/env python3\n{{ x }}\n",
		".gitignore":               "ignored.bash\n",
		"untracked/vendored.bash":  "echo no\n",
		"README.md":                "text\n",
		"scripts/not-a-gate.bash2": "x\n",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	if err := os.WriteFile(filepath.Join(root, "ignored.bash"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// sourceWrapper runs a bash snippet after sourcing the real wrapper.
func sourceWrapper(t *testing.T, snippet string) proc.Result {
	t.Helper()
	script := "set -euo pipefail\nsource \"$1/qa-discovery.bash\"\n" + snippet
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"bash", "-c", script, "bash", repoScripts(t)}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// D14
func TestTheWrapperFillsQAFilesFromTheToolOverARealTree(t *testing.T) {
	root := gitTree(t)
	for list, want := range map[string]string{
		"shell":                root + "/a.bash\n" + root + "/tool",
		"python":               root + "/py/b.py\n" + root + "/tests/helpers/test_h.py",
		"python-templates":     root + "/files/t.j2",
		"tracked-shell":        "a.bash\ntool",
		"tracked-python":       "py/b.py\ntests/helpers/test_h.py",
		"tracked-playbooks":    "playbooks/p.yml",
		"tracked-helper-tests": "tests/helpers/test_h.py",
	} {
		t.Run(list, func(t *testing.T) {
			res := sourceWrapper(t, "qa_discover "+list+" \""+root+"\"\nprintf '%s\\n' \"${QA_FILES[@]}\"\n")
			if res.Code != 0 || strings.TrimSpace(res.Stdout) != want {
				t.Fatalf("exit %d\nstdout %q\nstderr %s\nwant %q", res.Code, res.Stdout, res.Stderr, want)
			}
		})
	}
}

func TestTheWrapperKeepsSpacesAndLeavesAnEmptyListEmpty(t *testing.T) {
	root := gitTree(t)
	if err := os.WriteFile(filepath.Join(root, "sp ace.bash"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := sourceWrapper(t, "qa_discover shell \""+root+"\"\nprintf '[%s]' \"${QA_FILES[@]}\"\n")
	if res.Code != 0 || !strings.Contains(res.Stdout, "["+root+"/sp ace.bash]") {
		t.Fatalf("%+v", res)
	}
	if err := os.Remove(filepath.Join(root, "files/t.j2")); err != nil {
		t.Fatal(err)
	}
	res = sourceWrapper(t, "qa_discover python-templates \""+root+"\"\necho \"${#QA_FILES[@]}\"\n")
	if res.Code != 0 || strings.TrimSpace(res.Stdout) != "0" {
		t.Fatalf("%+v", res)
	}
}

func TestTheWrapperExitsTheCallerWhenTheListCannotBeMade(t *testing.T) {
	notGit := t.TempDir()
	res := sourceWrapper(t, "qa_discover shell \""+notGit+"\"\necho reached\n")
	if res.Code != 2 || strings.Contains(res.Stdout, "reached") || !strings.Contains(res.Stderr, "ERROR: cannot list the files git would publish") {
		t.Fatalf("%+v", res)
	}
}

func TestTheWrapperExclusionTestReadsTheRepositoryTrees(t *testing.T) {
	res := sourceWrapper(t, `qa_is_excluded roles/vendor/x/y.yml && echo shell-vendor
qa_is_excluded roles/own/y.yml || echo shell-own
qa_py_is_excluded .claude/ccy/plugins/p.py && echo py-plugin
qa_py_is_excluded .claude/ccy/claude-supervise.py || echo py-supervise
`)
	if res.Code != 0 || res.Stdout != "shell-vendor\nshell-own\npy-plugin\npy-supervise\n" {
		t.Fatalf("%+v", res)
	}
}

// D4, D9: on this repository, the lists agree with git and with each other.
func TestTheRealTreeListsAgreeWithGit(t *testing.T) {
	scripts := repoScripts(t)
	root := filepath.Dir(scripts)
	run := func(list string) []string {
		t.Helper()
		var out, errOut bytes.Buffer
		code := Main([]string{"--scripts", scripts, list}, Env{Stdout: &out, Stderr: &errOut, Cwd: root, Runner: proc.OS{}})
		if code != ExitOK {
			t.Fatalf("%s: exit %d: %s", list, code, errOut.String())
		}
		var paths []string
		for p := range strings.SplitSeq(out.String(), "\x00") {
			if p != "" {
				paths = append(paths, strings.TrimPrefix(p, root+"/"))
			}
		}
		return paths
	}
	for _, pair := range [][2]string{{"shell", "tracked-shell"}, {"python", "tracked-python"}} {
		found, tracked := run(pair[0]), run(pair[1])
		for _, rel := range tracked {
			if !slices.Contains(found, rel) {
				t.Errorf("%s misses the tracked file %s", pair[0], rel)
			}
		}
	}
	for _, list := range []string{"tracked-playbooks", "tracked-helper-tests"} {
		if len(run(list)) == 0 {
			t.Errorf("%s is empty on this repository", list)
		}
	}
}

// D15: git prints an untracked nested repository as one entry, "nested/", and lists none of its
// files; the run stops naming it rather than dropping the files from every gate.
func TestAnUntrackedNestedRepositoryStopsTheRunNamingIt(t *testing.T) {
	root := gitTree(t)
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: nested})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	for name, text := range map[string]string{"inner-tool": "#!/bin/bash\n", "inner.bash": "x\n"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "qa-trees.tsv"), []byte(defaultTrees), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, list := range []string{"shell", "python", "python-templates"} {
		t.Run(list, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Main([]string{"--root", root, list}, Env{Stdout: &out, Stderr: &errOut, Cwd: root, Runner: proc.OS{}})
			if code != ExitCannotRun || out.Len() != 0 || !strings.Contains(errOut.String(), "ERROR:") || !strings.Contains(errOut.String(), "nested/") {
				t.Fatalf("exit %d\nstdout %q\nstderr %s", code, out.String(), errOut.String())
			}
		})
	}
}

// D14: the bash reader refuses a table the Go refuses, one with no row for a language.
func TestTheBashTreesReaderRefusesATableMissingALanguage(t *testing.T) {
	for name, table := range map[string]string{"no python": "shell\ta\n", "no shell": "python\tb\n"} {
		t.Run(name, func(t *testing.T) {
			res := sourceTrees(t, table)
			if res.Code != 2 || strings.Contains(res.Stdout, "sourced") || !strings.Contains(res.Stderr, "qa-trees") {
				t.Fatalf("a table with %s was accepted: %+v", name, res)
			}
		})
	}
	if res := sourceTrees(t, "shell\ta\npython\tb\n"); res.Code != 0 || res.Stdout != "sourced\n" {
		t.Fatalf("a whole table was refused: %+v", res)
	}
}

// sourceTrees sources the real bash trees reader, through a link that sits beside the given table
// (the reader finds its table relative to the path it was sourced by).
func sourceTrees(t *testing.T, table string) proc.Result {
	t.Helper()
	w := newWorld(t)
	w.write("lib/keep", "", false)
	w.write("qa-trees.tsv", table, false)
	if err := os.Symlink(filepath.Join(repoScripts(t), "lib", "qa-trees.bash"), filepath.Join(w.root, "lib", "qa-trees.bash")); err != nil {
		t.Fatal(err)
	}
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"bash", "-c", "source \"$1/lib/qa-trees.bash\"; echo sourced", "bash", w.root}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}
