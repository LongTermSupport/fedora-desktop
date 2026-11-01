package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/archpin"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(archpin.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(archpin.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// projectFile reads a file of the real repository by its path from this package.
func projectFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// realRun runs the gate with the real git and semgrep over a work tree holding the project's own
// rule and fixture, the given allowlist and files. A machine without semgrep fails here with the
// gate's own message.
func realRun(t *testing.T, allowlist string, files map[string]string) (archpin.ExitCode, string, string) {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	files[".semgrep/arch-pin.yml"] = projectFile(t, ".semgrep/arch-pin.yml")
	files[".semgrep/arch-pin.fixture"] = projectFile(t, ".semgrep/arch-pin.fixture")
	files["scripts/arch-pin.allowlist"] = allowlist
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wrErr := os.WriteFile(path, []byte(text), 0o600); wrErr != nil {
			t.Fatal(wrErr)
		}
	}
	var stdout, stderr bytes.Buffer
	code := archpin.Main([]string{"--root", root}, archpin.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// The selection shape the Dockerfiles use passes; a bare one-architecture URL fails, and is held
// by the ratchet once listed. Prose and the rule's own files are never reported.
func TestTheRealEngineHoldsTheRatchet(t *testing.T) {
	selected := "RUN case \"$(dpkg --print-architecture)\" in \\\n" +
		"      amd64) asset=yq_linux_amd64 ;; \\\n" +
		"      arm64) asset=yq_linux_arm64 ;; \\\n" +
		"    esac\n"
	pinned := "RUN wget -qO /usr/bin/yq https://example.com/yq_linux_amd64\n"
	files := func() map[string]string {
		return map[string]string{
			"a/Dockerfile": selected,
			"b/Dockerfile": "FROM x\n" + pinned,
			"docs/x.md":    pinned,
		}
	}
	code, stdout, stderr := realRun(t, "# ratchet\n", files())
	if code != archpin.ExitFailed || !strings.Contains(stdout, "b/Dockerfile:2  arch-pinned-artefact") || strings.Contains(stdout, "a/Dockerfile") || strings.Contains(stdout, "docs/x.md") || !strings.Contains(stdout, "passed: 1 failed: 1") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, "b/Dockerfile\t1\tyq: arm64 build exists\n", files())
	if code != archpin.ExitOK || !strings.Contains(stdout, "1 known instance(s) in 1 file(s)") || !strings.Contains(stdout, "passed: 2 failed: 0") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
