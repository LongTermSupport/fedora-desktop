package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ghscopes"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(ghscopes.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(ghscopes.ExitCannotRun) {
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
// rule and fixture plus files. A machine without semgrep fails here with the gate's own message.
func realRun(t *testing.T, files map[string]string) (ghscopes.ExitCode, string, string) {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	files[".semgrep/boot-hold.yml"] = projectFile(t, ".semgrep/boot-hold.yml")
	files[".semgrep/boot-hold.fixture"] = projectFile(t, ".semgrep/boot-hold.fixture")
	files["scripts/qa-trees.tsv"] = projectFile(t, "scripts/qa-trees.tsv")
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
	code := ghscopes.BootHold.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// The issue #263 shape, and the same unit ordered after the target and started at once. The
// timeout is joined on so that this file is not itself a finding.
const (
	heldUnit = "[Unit]\nDescription=verify\n\n[Service]\nType=oneshot\nTimeoutStartSec=" + "30min\n" +
		"ExecStart=/usr/local/sbin/verify\n\n[Install]\nWantedBy=multi-user.target\n"
	freeUnit = "[Unit]\nDescription=verify\nAfter=multi-user.target\n\n[Service]\nType=exec\nRuntimeMaxSec=30min\n" +
		"ExecStart=/usr/local/sbin/verify\n\n[Install]\nWantedBy=multi-user.target\n"
)

func TestTheRealEngineFindsTheHeldTargetAndPassesTheFreeOne(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{"files/good.service": freeUnit, "files/bad.service": heldUnit})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "files/bad.service:1  unit-holds-boot-target") ||
		strings.Contains(stdout, "files/good.service") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"files/good.service": freeUnit})
	if code != ghscopes.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
