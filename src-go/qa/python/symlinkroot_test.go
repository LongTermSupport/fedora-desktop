package python

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
	w := newWorld(t, map[string]string{"bad.py": "SYNTAX_ERROR\n"})
	link := filepath.Join(t.TempDir(), "view")
	if err := os.Symlink(w.root, link); err != nil {
		t.Fatal(err)
	}
	w.run.compile = func(files []string) proc.Result {
		lines := make([]string, 0, len(files))
		for _, file := range files {
			printed, err := filepath.EvalSymlinks(file)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, answer(file, "  File \""+printed+"\", line 1\nSyntaxError: invalid syntax"))
		}
		return proc.Result{Stdout: strings.Join(lines, "\n") + "\n"}
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
}

func TestARootThatCannotBeResolvedIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	got := w.main("--root", filepath.Join(w.root, "missing"))
	wantExit(t, got, ExitCannotRun)
	if !strings.Contains(got.stderr, "cannot resolve the repository root") {
		t.Fatalf("stderr:\n%s", got.stderr)
	}
	w.noReport()
}
