package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/toolchain"
)

const pins = "RUFF=0.16.8\nSEMGREP=1.177.0\nSHELLCHECK=0.11.0\nGOLANGCI_LINT=2.14.0\n"

// ownerExec is the one bit a fake tool needs beyond a private file.
var ownerExec = os.FileMode(0o100)

// fakeTool writes an executable shell script named name into dir that runs body.
func fakeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600|ownerExec); err != nil {
		t.Fatal(err)
	}
}

// machine is a repository root with the pins and a go.mod floor every real go satisfies.
func machine(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range map[string]string{".qa-versions": pins, "src-go/go.mod": "module m\n\ngo 1.1\n", "src-go/qa/go.mod": "module m\n\ngo 1.1\n"} {
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

// toolsOnPath makes PATH one directory of fake tools printing the given --version text, plus the
// real go (linked in) so GOVERSION is read from a real process. It returns that directory.
func toolsOnPath(t *testing.T, versions map[string]string) string {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for name, out := range versions {
		fakeTool(t, bin, name, "printf '%s\\n' '"+out+"'")
	}
	if err := os.Symlink(realGo, filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return bin
}

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(toolchain.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(toolchain.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	if code := run([]string{"--root", t.TempDir()}); code != int(toolchain.ExitCannotRun) {
		t.Fatalf("a root without .qa-versions: exit %d", code)
	}
}

// T10, T15: real processes, a real PATH lookup and the real go.
func TestRealToolsAreReadFromPathAndMatch(t *testing.T) {
	root := machine(t)
	toolsOnPath(t, map[string]string{
		"ruff": "ruff 0.16.8", "semgrep": "1.177.0",
		"shellcheck":    "ShellCheck - x\nversion: 0.11.0\nlicense: y",
		"golangci-lint": "golangci-lint has version 2.14.0 built with go1.26 from abc",
	})
	if code := run([]string{"--root", root}); code != int(toolchain.ExitOK) {
		t.Fatalf("exit %d", code)
	}
}

// T2, T9, T11: tool drift is exit 1, never the exit 2 that aborts qa-all.
func TestADriftedOrAbsentToolIsExitOneNotTwo(t *testing.T) {
	root := machine(t)
	toolsOnPath(t, map[string]string{"ruff": "ruff 0.1.0", "semgrep": "1.177.0", "shellcheck": "version: 0.11.0"})
	if code := run([]string{"--root", root}); code != int(toolchain.ExitFailed) {
		t.Fatalf("exit %d, want 1 (golangci-lint absent, ruff drifted)", code)
	}
}

// T10: a non-zero exit is not a match even when the number it printed is the pin.
func TestAToolThatFailsItsVersionRunIsNotAMatch(t *testing.T) {
	root := machine(t)
	bin := toolsOnPath(t, map[string]string{"semgrep": "1.177.0", "shellcheck": "version: 0.11.0", "golangci-lint": "golangci-lint has version 2.14.0"})
	fakeTool(t, bin, "ruff", "echo 'ruff 0.16.8'; echo broken >&2; exit 3")
	if code := run([]string{"--root", root}); code != int(toolchain.ExitFailed) {
		t.Fatalf("a tool exiting 3 after printing the pinned version: exit %d, want 1", code)
	}
}

// T6: a go directive above the installed go fails the floor, with the real go.
func TestARealGoBelowTheFloorFails(t *testing.T) {
	root := machine(t)
	if err := os.WriteFile(filepath.Join(root, "src-go", "go.mod"), []byte("module m\n\ngo 99.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toolsOnPath(t, map[string]string{
		"ruff": "ruff 0.16.8", "semgrep": "1.177.0", "shellcheck": "version: 0.11.0",
		"golangci-lint": "golangci-lint has version 2.14.0",
	})
	if code := run([]string{"--root", root}); code != int(toolchain.ExitFailed) {
		t.Fatalf("exit %d, want 1", code)
	}
}
