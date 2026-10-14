package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansible"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(ansible.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(ansible.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root with no Ansible directories is a run that could not complete.
	if code := run([]string{"--root", t.TempDir()}); code != int(ansible.ExitCannotRun) {
		t.Fatalf("empty root: exit %d", code)
	}
}

// gitRepo makes a git work tree holding the given empty files, with a .gitignore of the given lines.
func gitRepo(t *testing.T, ignore string, files ...string) string {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(ignore), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGitIgnoredListsOnlyIgnoredEntriesInTheScannedDirectories(t *testing.T) {
	root := gitRepo(t, "secret.yml\nignored-dir/\nsecret-link\n",
		"tasks/a.yml", "tasks/secret.yml", "roles/ignored-dir/x.yml", "outside/secret.yml")
	if err := os.Symlink("nowhere", filepath.Join(root, "tasks", "secret-link")); err != nil {
		t.Fatal(err)
	}
	got, err := gitIgnored(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"roles/ignored-dir/x.yml", "tasks/secret-link", "tasks/secret.yml"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGitIgnoredOutsideAWorkTreeIsAnError(t *testing.T) {
	if _, err := gitIgnored(t.TempDir()); err == nil {
		t.Fatal("a directory that is not a git work tree was read as having nothing ignored")
	}
}
