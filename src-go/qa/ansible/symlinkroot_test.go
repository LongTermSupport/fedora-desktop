package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root reached through a symlink is resolved once (qa/internal/rootpath): findings are named
// from the root, and no checkout path reaches stdout or the report.
func TestARootReachedThroughASymlinkNeverLeaksTheCheckoutPath(t *testing.T) {
	files := cleanTree()
	files["environment/host_vars/leak.yml"] = "  failed_when: false\n"
	root := writeTree(t, files)
	chmodX(t, root, "playbooks/a.yml")
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(resolved, link); err != nil {
		t.Fatal(err)
	}
	res := runIn(t, root, "--root", link)
	if res.code != ExitFailed {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", res.code, res.out, res.errOut)
	}
	for name, text := range map[string]string{"stdout": res.out, "stderr": res.errOut, "report": res.report} {
		if strings.Contains(text, resolved) || strings.Contains(text, link) {
			t.Fatalf("a checkout path leaked in %s:\n%s", name, text)
		}
	}
	if !strings.Contains(res.out, "environment/host_vars/leak.yml") {
		t.Fatalf("stdout:\n%s", res.out)
	}
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	res := runIn(t, t.TempDir(), "--root", filepath.Join(t.TempDir(), "missing"))
	if res.code != ExitCannotRun || !strings.Contains(res.errOut, "cannot resolve the repository root") {
		t.Fatalf("exit %d\nstderr:\n%s", res.code, res.errOut)
	}
}
