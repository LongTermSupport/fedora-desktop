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
	files[".semgrep/argv-leak.yml"] = projectFile(t, ".semgrep/argv-leak.yml")
	files[".semgrep/argv-leak.fixture.yml"] = projectFile(t, ".semgrep/argv-leak.fixture.yml")
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
	code := ghscopes.ArgvLeak.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// The issue #220 shapes, and the same work done with the secret kept off every command line.
const (
	argvScript     = "#!/bin/bash\nssh-keygen -y -P \"$(cat \"$pp_file\")\" -f \"$key\"\n"
	promptedScript = "#!/bin/bash\nSSH_ASKPASS=\"$dir/askpass\" SSH_ASKPASS_REQUIRE=force ssh-keygen -y -f \"$key\" </dev/null\n"
	envPlay        = "- name: Pin\n  ansible.builtin.command:\n    argv: [python3, -m, helper]\n  environment:\n    ADMIN_PASSWORD: \"{{ admin_password }}\"\n"
	stdinPlay      = "- name: Pin\n  ansible.builtin.command:\n    argv: [python3, -m, helper]\n    stdin: \"{{ admin_password }}\"\n"
)

func TestTheRealEngineFindsTheSecretOnTheCommandLineAndPassesStdin(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{
		"clean/askpass.bash": promptedScript,
		"clean/play.yml":     stdinPlay,
		"argv/rekey.bash":    argvScript,
		"argv/play.yml":      envPlay,
		// Daemon-deployed: the trees table keeps them out of every gate (qa-trees-drift).
		".claude/init.sh": argvScript,
	})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "argv/rekey.bash:2  secret-in-argv") ||
		!strings.Contains(stdout, "argv/play.yml:5  secret-in-argv") || !strings.Contains(stdout, "passed: 3 failed: 2") ||
		strings.Contains(stdout, ".claude/") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"clean/askpass.bash": promptedScript, "clean/play.yml": stdinPlay})
	if code != ghscopes.ExitOK || !strings.Contains(stdout, "passed: 3 failed: 0") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
