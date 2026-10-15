package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root reached through a symlink (or spelt relative) is resolved once (qa/internal/rootpath),
// so the absolute paths printed are under the real root, the one a gate strips from what an
// engine prints.
func TestARootReachedThroughASymlinkPrintsPathsUnderTheRealRoot(t *testing.T) {
	w := newWorld(t)
	w.track("scripts/a.bash", bashShebang)
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	got := w.main("--root", link, "shell")
	wantExit(t, got, ExitOK)
	wantPaths(t, got, w.root+"/scripts/a.bash")
	if strings.Contains(got.stdout, link) {
		t.Fatalf("the link spelling leaked: %q", got.stdout)
	}
	wantPaths(t, w.main("--root", ".", "shell"), w.root+"/scripts/a.bash")
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := newWorld(t)
	got := w.main("--root", filepath.Join(w.root, "missing"), "shell")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "cannot resolve the repository root")
}
