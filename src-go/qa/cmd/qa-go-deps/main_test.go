package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/godeps"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(godeps.ExitOK) {
		t.Fatalf("--help: code %d", code)
	}
	if code := run([]string{"--bogus"}); code != int(godeps.ExitCannotRun) {
		t.Fatalf("--bogus: code %d", code)
	}
}

// repoWithNoDependencies is a repository whose src-go module requires nothing, which real go
// judges offline: nothing to verify, nothing to tidy, nothing to vendor.
func repoWithNoDependencies(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range map[string]string{
		"src-go/go.mod":            "module ex.com/me\n\ngo 1.26.0\n",
		"src-go/me.go":             "package me\n",
		"src-go/go-deps.allowlist": "# nothing is allowed\n",
	} {
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

// The real go commands, through a symlinked root (rootpath resolves it before go sees it).
func TestRealGoPassesAModuleWithNoDependenciesThroughASymlinkedRoot(t *testing.T) {
	root := repoWithNoDependencies(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", link}); code != int(godeps.ExitOK) {
		t.Fatalf("code %d", code)
	}
}

// A vendor directory go mod vendor would not write is a finding, judged by the real go.
func TestRealGoFindsAStrayVendorTree(t *testing.T) {
	root := repoWithNoDependencies(t)
	stray := filepath.Join(root, "src-go", "vendor", "modules.txt")
	if err := os.MkdirAll(filepath.Dir(stray), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("# ex.com/gone v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--root", root}); code != int(godeps.ExitFailed) {
		t.Fatalf("code %d", code)
	}
}
