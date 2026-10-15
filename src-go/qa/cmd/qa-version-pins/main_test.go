package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/versionpins"
)

const row = `  - playbook: playbooks/a.yml
    var: a_version
    github: o/r
    installed:
      kind: command
      name: a
`

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(versionpins.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(versionpins.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// realRun runs the gate over a real work tree holding the files.
func realRun(t *testing.T, root string, files map[string]string) (versionpins.ExitCode, string, string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	env := versionpins.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Resolve: filepath.EvalSymlinks, Open: os.DirFS}
	return versionpins.Main([]string{"--root", root}, env), stdout.String(), stderr.String()
}

// V3, V18, V23 on the real file system: a root reached through a symlink prints no checkout path,
// and a playbook that is a symlink (which os.DirFS would follow) is refused.
func TestTheRealFileSystemASymlinkedRootAndASymlinkedPlaybook(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"vars/version-pins.yml": "version_pins:\n" + row, "playbooks/a.yml": "a_version: \"1\"\n"}
	code, stdout, stderr := realRun(t, link, files)
	if code != versionpins.ExitOK || !strings.Contains(stdout, "VERSION-PINS-OK 1 pin(s), 1 with install state tracked") ||
		strings.Contains(stdout+stderr, link) || strings.Contains(stdout+stderr, target) {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}

	if err := os.Remove(filepath.Join(target, "playbooks/a.yml")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("a_version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "playbooks/a.yml")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = realRun(t, link, nil)
	if code != versionpins.ExitFailed || stdout != "" || !strings.Contains(stderr, "playbooks/a.yml is not a regular file") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
}

// V4 on the real file system: a manifest that is a symlink is refused, never read through.
func TestARealSymlinkedManifestIsExitTwo(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(t.TempDir(), "elsewhere.yml")
	if err := os.WriteFile(other, []byte("version_pins:\n"+row), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vars"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "vars/version-pins.yml")); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := realRun(t, root, map[string]string{"playbooks/a.yml": "a_version: 1\n"})
	if code != versionpins.ExitCannotRun || !strings.Contains(stderr, "vars/version-pins.yml is a symlink") {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
}

// The repository's own manifest passes with every declared pin resolved: the gate is also a unit
// test of the live tree.
func TestTheLiveManifestResolves(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := realRun(t, root, nil)
	if code != versionpins.ExitOK || !strings.Contains(stdout, "VERSION-PINS-OK") || stderr != "" {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
}

// V6: an empty manifest on disk is exit 2 for being empty.
func TestARealEmptyManifestIsExitTwo(t *testing.T) {
	code, _, stderr := realRun(t, t.TempDir(), map[string]string{"vars/version-pins.yml": "version_pins: []\n"})
	if code != versionpins.ExitCannotRun || !strings.Contains(stderr, "declares no pins; nothing was checked") {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
}
