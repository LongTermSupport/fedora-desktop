package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/nokill"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(nokill.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(nokill.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root that is not a git work tree is a run that could not complete (E3).
	if code := run([]string{"--root", t.TempDir()}); code != int(nokill.ExitCannotRun) {
		t.Fatalf("not a work tree: exit %d", code)
	}
}

// gitRepo makes a git work tree with a .gitignore of the given lines and the given files.
func gitRepo(t *testing.T, ignore string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	files[".gitignore"] = ignore
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

// E3: the files git would publish under the watchdog's directories: untracked-not-ignored
// included, ignored left out, files elsewhere left out.
func TestPublishedListsWhatGitWouldPublishUnderTheTargets(t *testing.T) {
	root := gitRepo(t, "secret.py\nignored-dir/\n", map[string]string{
		"helpers/containerwatch/core.py":                              "",
		"helpers/containerwatch/secret.py":                            "",
		"helpers/containerwatch/ignored-dir/x.py":                     "",
		"helpers/containerwatch/sub/deep.py":                          "",
		"helpers/elsewhere/other.py":                                  "",
		"extensions/fedora-desktop@fedora-desktop/containerReport.js": "",
		"extensions/fedora-desktop@fedora-desktop/other.js":           "",
	})
	got, err := published(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{
		"extensions/fedora-desktop@fedora-desktop/containerReport.js",
		"helpers/containerwatch/core.py",
		"helpers/containerwatch/sub/deep.py",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublishedOutsideAWorkTreeIsAnError(t *testing.T) {
	if _, err := published(t.TempDir()); err == nil {
		t.Fatal("a directory that is not a git work tree was read as publishing nothing")
	}
}

// End to end on a real work tree and the real file system: a clean watchdog passes, then a
// kill call site in an untracked file fails it, and the same call in an ignored file does not.
func TestEndToEndOnARealWorkTree(t *testing.T) {
	files := map[string]string{
		"helpers/containerwatch/core.py":                                  "def f():\n    return 1\n",
		"extensions/fedora-desktop@fedora-desktop/containerReport.js":     "function a() {}\n",
		"extensions/fedora-desktop@fedora-desktop/sections/containers.js": "function b() {}\n",
	}
	root := gitRepo(t, "ignored.py\n", files)
	if code := run([]string{"--root", root}); code != int(nokill.ExitOK) {
		t.Fatalf("clean tree: exit %d", code)
	}
	write := func(rel, text string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("helpers/containerwatch/ignored.py", "os.kill(1, 9)\n")
	if code := run([]string{"--root", root}); code != int(nokill.ExitOK) {
		t.Fatalf("an ignored file was judged: exit %d", code)
	}
	write("helpers/containerwatch/bad.py", "os.kill(1, 9)\n")
	if code := run([]string{"--root", root}); code != int(nokill.ExitFailed) {
		t.Fatalf("an untracked kill call site passed: exit %d", code)
	}
}
