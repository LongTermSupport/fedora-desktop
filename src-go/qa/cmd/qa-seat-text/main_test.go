package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/seattext"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(seattext.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(seattext.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	if code := run([]string{"--root", t.TempDir()}); code != int(seattext.ExitCannotRun) {
		t.Fatalf("not a work tree: exit %d", code)
	}
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// End to end on a real work tree: a clean doc passes, an ignored doc is not judged, and a
// published one with a seat to type fails.
func TestEndToEndOnARealWorkTree(t *testing.T) {
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	write(t, root, ".gitignore", "docs/ignored.md\n")
	write(t, root, "src-go/qa/seattext/allowed.tsv", "# none\n")
	write(t, root, "docs/a.md", "run `ccy --teams <team>`\n")
	if code := run([]string{"--root", root}); code != int(seattext.ExitOK) {
		t.Fatalf("clean tree: exit %d", code)
	}
	write(t, root, "docs/ignored.md", "ccy --teams 4@team-a\n")
	if code := run([]string{"--root", root}); code != int(seattext.ExitOK) {
		t.Fatalf("an ignored file was judged: exit %d", code)
	}
	write(t, root, "docs/b.md", "ccy --teams 4@team-a\n")
	if code := run([]string{"--root", root}); code != int(seattext.ExitFailed) {
		t.Fatalf("a seat to type passed: exit %d", code)
	}
}
