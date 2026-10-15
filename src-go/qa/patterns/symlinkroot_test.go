package patterns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root reached through a symlink is resolved once (qa/internal/rootpath): the real path of a
// link's target is under the root the gate strips, so no checkout path reaches stdout or the
// report.
func TestARootReachedThroughASymlinkNeverLeaksTheCheckoutPath(t *testing.T) {
	w := oneScript(t)
	w.write("untracked/scratch/evil.bash", shebang+"cmd || true\n")
	w.link("scripts/alias.bash", "../untracked/scratch/evil.bash")
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	got := w.main("--root", link)
	wantExit(t, got, ExitOK)
	raw, err := os.ReadFile(w.out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.stdout, w.root) || strings.Contains(string(raw), w.root) ||
		strings.Contains(got.stdout, link) || strings.Contains(string(raw), link) {
		t.Fatalf("a checkout path leaked:\nstdout:\n%s\nreport:\n%s", got.stdout, raw)
	}
	if !strings.Contains(got.stdout, "    scripts/alias.bash -> untracked/scratch/evil.bash") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := oneScript(t)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	wantExit(t, got, ExitCannotRun)
	if !strings.Contains(got.stderr, "cannot resolve the repository root") {
		t.Fatalf("stderr:\n%s", got.stderr)
	}
}
