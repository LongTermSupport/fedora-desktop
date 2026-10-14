package ansiblesyntax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root reached through a symlink is resolved once (qa/internal/rootpath) and the root is
// stripped from the engine's error, so no checkout path reaches stdout or the report whichever
// spelling of the path the engine prints.
func TestARootReachedThroughASymlinkNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, nil)
	w.addPlaybook("a.yml")
	w.addFile("b.yml", "- hosts: all\n  SYNTAX_ERROR\n")
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	got := w.main("--root", link)
	wantExit(t, got, ExitFailed)
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.stdout, w.root) || strings.Contains(string(raw), w.root) ||
		strings.Contains(got.stdout, link) || strings.Contains(string(raw), link) {
		t.Fatalf("a checkout path leaked:\nstdout:\n%s\nreport:\n%s", got.stdout, raw)
	}
	if !strings.Contains(got.stdout, "ERROR! bad syntax in b.yml\n") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "cannot resolve the repository root")
	w.noReport()
}
