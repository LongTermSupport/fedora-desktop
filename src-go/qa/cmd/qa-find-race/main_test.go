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
	files[".semgrep/find-race.yml"] = projectFile(t, ".semgrep/find-race.yml")
	files[".semgrep/find-race.fixture"] = projectFile(t, ".semgrep/find-race.fixture")
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
	code := ghscopes.FindRace.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// The issue #152 probe, as the play had it, and the guarded form the play has now.
const (
	unguardedProbe = "- name: Find\n  ansible.builtin.command:\n    argv:\n      - find\n      - /store\n" +
		"      - -xdev\n      - -ignore_readdir_race\n      - \"!\"\n      - -uid\n      - \"0\"\n      - -print\n"
	guardedProbe = "- name: Find\n  ansible.builtin.command:\n    argv:\n      - find\n      - /store\n" +
		"      - -xdev\n      - -ignore_readdir_race\n      - -links\n      - +0\n      - \"!\"\n      - -uid\n      - \"0\"\n      - -print\n"
)

func TestTheRealEngineFindsTheUnguardedProbeAndPassesTheGuardedOne(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{
		"tasks/clean.yml": guardedProbe,
		"tasks/probe.yml": unguardedProbe,
		"scripts/a.bash":  "find \"$d\" -ignore_readdir_race -links +0 ! -user root -print\n",
	})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "tasks/probe.yml:7  find-race-negated-stat-test") || !strings.Contains(stdout, "passed: 2 failed: 1") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"tasks/clean.yml": guardedProbe})
	if code != ghscopes.ExitOK || !strings.Contains(stdout, "passed: 1 failed: 0") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
