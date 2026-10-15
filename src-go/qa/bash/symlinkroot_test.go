package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// A root reached through a symlink is resolved once (qa/internal/rootpath), so the real path an
// engine prints is the root the gate strips: no checkout path reaches stdout or the report.
func TestARootReachedThroughASymlinkNeverLeaksTheCheckoutPath(t *testing.T) {
	w := newWorld(t, nil)
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	printed := filepath.Join(w.root, "tool.bash")
	w.run.lint = func([]string) proc.Result {
		return lintAnswer(finding(printed, 3, "error", 1000, "boom"))
	}
	got := w.main("--root", link)
	expect(t, got, ExitFailed)
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.stdout, w.root) || strings.Contains(string(raw), w.root) ||
		strings.Contains(got.stdout, link) || strings.Contains(string(raw), link) {
		t.Fatalf("a checkout path leaked:\nstdout:\n%s\nreport:\n%s", got.stdout, raw)
	}
	mustContain(t, "stdout", got.stdout, "tool.bash")
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	expect(t, got, ExitCannotRun)
	mustContain(t, "stderr", got.stderr, "cannot resolve the repository root")
	w.noReport()
}
