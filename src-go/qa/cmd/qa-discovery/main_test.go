package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "usage: qa-discovery") {
		t.Fatalf("--help: exit %d %q", code, out.String())
	}
	if code := run([]string{"--nope"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "ERROR:") {
		t.Fatalf("--nope: exit %d %q", code, errOut.String())
	}
}

// TestRunOnARealGitTree runs the command over a real work tree: an untracked file that is not
// ignored is listed, an ignored one is not, a tracked file removed from the tree is not.
func TestRunOnARealGitTree(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: append([]string{"git"}, args...), Dir: root})
		if err != nil || res.Code != 0 {
			t.Fatalf("git %v: %v %+v", args, err, res)
		}
	}
	git("init", "-q")
	files := map[string]string{
		"scripts/qa-trees.tsv": "shell\tskipme\npython\tskipme\n",
		"scripts/tracked.bash": "echo hi\n",
		"scripts/gone.bash":    "echo gone\n",
		"tool":                 "#!/usr/bin/env bash\n",
		"skipme/x.bash":        "x\n",
		".gitignore":           "ignored.bash\n",
	}
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	if err := os.Remove(filepath.Join(root, "scripts/gone.bash")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"ignored.bash", "fresh.bash"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"--root", root, "shell"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var want strings.Builder
	for _, rel := range []string{"fresh.bash", "scripts/tracked.bash", "tool"} {
		want.WriteString(root + "/" + rel + "\x00")
	}
	if out.String() != want.String() {
		t.Fatalf("got %q want %q", out.String(), want.String())
	}
}
