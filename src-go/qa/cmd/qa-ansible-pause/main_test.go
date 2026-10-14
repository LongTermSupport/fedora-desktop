package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansiblepause"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const trees = "shell\t.git\nshell\tuntracked\npython\t.venv\n"

const (
	badTask   = "- name: a\n  pause:\n    seconds: 1\n"
	cleanTask = "- name: a\n  debug:\n    msg: hi\n"
)

// gitRepo makes a git work tree with the trees table, a .gitignore and the given files.
func gitRepo(t *testing.T, ignore string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	files[".gitignore"] = ignore
	files["scripts/qa-trees.tsv"] = trees
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(ansiblepause.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(ansiblepause.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	if code := run([]string{"--root", t.TempDir()}); code != int(ansiblepause.ExitCannotRun) {
		t.Fatalf("not a work tree: exit %d", code)
	}
}

// E4: the discovery lists published YAML; only playbooks/ and tasks/ are judged.
func TestYamlListsWhatTheSharedDiscoveryPublishes(t *testing.T) {
	root := gitRepo(t, "ignored.yml\n", map[string]string{
		"playbooks/a.yml":        cleanTask,
		"playbooks/ignored.yml":  badTask,
		"tasks/deep/b.yaml":      cleanTask,
		"vars/c.yml":             badTask,
		"tests/fixtures/f.yml":   badTask,
		"playbooks/untracked.md": "",
	})
	got, err := yaml(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"playbooks/a.yml", "tasks/deep/b.yaml"} {
		if !slices.Contains(got, want) {
			t.Fatalf("%s missing from %q", want, got)
		}
	}
	if slices.Contains(got, "playbooks/ignored.yml") || slices.Contains(got, "playbooks/untracked.md") {
		t.Fatalf("an ignored or non-YAML file was listed: %q", got)
	}
	for _, p := range got {
		if filepath.IsAbs(p) {
			t.Fatalf("%s is not relative to the root", p)
		}
	}
}

// E3: an absolute PATH reached through a symlinked checkout is inside the root once both are
// resolved; a symlink inside the tree to a readable file is read as that file (E6).
func TestAbsolutePathThroughASymlinkedCheckoutIsInsideTheRoot(t *testing.T) {
	root := gitRepo(t, "", map[string]string{"playbooks/a.yml": cleanTask, "playbooks/bad.yml": badTask})
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root, filepath.Join(link, "playbooks", "bad.yml")}); code != int(ansiblepause.ExitFailed) {
		t.Fatalf("path through a symlinked checkout, real root: exit %d", code)
	}
	if code := run([]string{"--root", link, filepath.Join(root, "playbooks", "bad.yml")}); code != int(ansiblepause.ExitFailed) {
		t.Fatalf("real path, symlinked root: exit %d", code)
	}
	if code := run([]string{"--root", link, filepath.Join(link, "playbooks", "a.yml")}); code != int(ansiblepause.ExitOK) {
		t.Fatalf("clean file through the link: exit %d", code)
	}
	if code := run([]string{"--root", root, filepath.Join(t.TempDir(), "x.yml")}); code != int(ansiblepause.ExitCannotRun) {
		t.Fatalf("a file outside the root: exit %d", code)
	}
}

func TestASymlinkedPlaybookIsReadAsItsTarget(t *testing.T) {
	root := gitRepo(t, "", map[string]string{"playbooks/a.yml": cleanTask, "elsewhere/real.yml": badTask})
	if err := os.Symlink(filepath.Join(root, "elsewhere", "real.yml"), filepath.Join(root, "playbooks", "linked.yml")); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root, "playbooks/linked.yml"}); code != int(ansiblepause.ExitFailed) {
		t.Fatalf("symlinked playbook with a bare pause: exit %d", code)
	}
}

// End to end on a real work tree: clean passes, then a pause in an untracked playbook fails, and
// the same in an ignored one or outside playbooks/ and tasks/ does not.
func TestEndToEndOnARealWorkTree(t *testing.T) {
	root := gitRepo(t, "playbooks/ignored.yml\n", map[string]string{
		"playbooks/a.yml": cleanTask,
		"vars/c.yml":      badTask,
	})
	if code := run([]string{"--root", root}); code != int(ansiblepause.ExitOK) {
		t.Fatalf("clean tree: exit %d", code)
	}
	write := func(rel, text string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("playbooks/ignored.yml", badTask)
	if code := run([]string{"--root", root}); code != int(ansiblepause.ExitOK) {
		t.Fatalf("an ignored file was judged: exit %d", code)
	}
	write("playbooks/bad.yml", badTask)
	if code := run([]string{"--root", root}); code != int(ansiblepause.ExitFailed) {
		t.Fatalf("an untracked pause passed: exit %d", code)
	}
}
