package nokill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root reached through a symlink is resolved once (qa/internal/rootpath): the file system and
// the git listing are asked for the real root, and no checkout path reaches the output.
func TestARootReachedThroughASymlinkIsResolvedAndNeverLeaksTheCheckoutPath(t *testing.T) {
	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	w := cleanWorld()
	w.set("helpers/containerwatch/bad.py", "import os\nos.kill(1, 9)\n")
	env := w.env()
	env.Resolve = filepath.EvalSymlinks
	var asked string
	env.Published = func(root string) ([]string, error) {
		asked = root
		return w.published, nil
	}
	if code := Main([]string{"--root", link}, env); code != ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, w.out.String(), w.err.String())
	}
	if asked != realRoot {
		t.Fatalf("git was asked about %q, want the real root %q", asked, realRoot)
	}
	if all := w.out.String() + w.err.String(); strings.Contains(all, realRoot) || strings.Contains(all, link) {
		t.Fatalf("a checkout path leaked:\n%s", all)
	}
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := cleanWorld()
	env := w.env()
	env.Resolve = filepath.EvalSymlinks
	missing := filepath.Join(t.TempDir(), "missing")
	if code := Main([]string{"--root", missing}, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "cannot resolve the repository root") {
		t.Fatalf("stderr:\n%s", w.err.String())
	}
}
