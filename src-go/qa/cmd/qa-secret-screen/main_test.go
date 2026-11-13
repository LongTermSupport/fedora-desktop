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
// rules and fixtures plus files. A machine without semgrep fails here with the gate's own message.
func realRun(t *testing.T, files map[string]string) (ghscopes.ExitCode, string, string) {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	for _, rel := range []string{
		".semgrep/secret-screen.yml", ".semgrep/secret-screen.bash",
		".semgrep/secret-screen.py", ".semgrep/secret-screen.fixture.yml", "scripts/qa-trees.tsv",
	} {
		files[rel] = projectFile(t, rel)
	}
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
	code := ghscopes.SecretScreen.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// The vault.bash get shape (issue #261) and its fix. The function name is joined on so that this
// file is not itself a finding.
var (
	bareGet   = "cmd_get() {\n  " + "_decrypt" + "_var \"$varname\"\n  echo\n}\n"
	screenGet = "cmd_get() {\n  " + "_decrypt" + "_var \"$varname\" | scripts/secret-screen.bash\n  echo\n}\n"
)

func TestTheRealEngineFindsTheBareSecretAndPassesTheScreenedOne(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{"good.bash": screenGet, "bad.bash": bareGet})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "bad.bash:2  secret-on-normal-screen") ||
		strings.Contains(stdout, "good.bash") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"good.bash": screenGet})
	if code != ghscopes.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
