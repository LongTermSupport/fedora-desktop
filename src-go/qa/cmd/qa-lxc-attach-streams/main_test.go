package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/lxcattach"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(lxcattach.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(lxcattach.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
}

// gitRepo is a real git work tree holding the files, all of them added to the index.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"git", "-c", "core.hooksPath=/dev/null", "init", "-q"}, {"git", "-c", "core.hooksPath=/dev/null", "add", "-A"}} {
		res, err := proc.OS{}.Run(t.Context(), proc.Cmd{Argv: argv, Dir: root})
		if err != nil || res.Code != 0 {
			t.Fatalf("%v: %v %s", argv, err, res.Stderr)
		}
	}
	return root
}

// The real tracked list over a real git tree: an untracked script is not judged, a tracked one
// with a finding fails the run.
func TestTheTrackedShellScriptsOfARealTreeAreJudged(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "..", lxcattach.FixturePath))
	if err != nil {
		t.Fatal(err)
	}
	trees, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "scripts", "qa-trees.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	root := gitRepo(t, map[string]string{
		lxcattach.FixturePath:  string(fixture),
		"scripts/qa-trees.tsv": string(trees),
		"scripts/qa-x.bash":    "#!/usr/bin/env bash\ntrue\n",
		"scripts/bad.bash":     "#!/usr/bin/env bash\nsudo lxc-attach -n c -- true 2>\"$e\"\n",
	})
	var stdout, stderr bytes.Buffer
	code := lxcattach.Main([]string{"--root", root}, newEnv(&stdout, &stderr, root))
	if code != lxcattach.ExitFailed || !strings.Contains(stderr.String(), "scripts/bad.bash:2: lxc-attach-file-stream: stderr is a file") ||
		!strings.Contains(stderr.String(), "across 2 shell script(s) examined") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(root, "scripts/bad.bash"), []byte("#!/usr/bin/env bash\nsudo lxc-attach -n c -- true 2>&1 | cat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = lxcattach.Main([]string{"--root", root}, newEnv(&stdout, &stderr, root))
	if code != lxcattach.ExitOK || !strings.Contains(stdout.String(), "LXC-ATTACH-STREAMS-OK: 2 shell script(s) examined") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
}

func TestADiscoveryThatFailsCannotRun(t *testing.T) {
	root := t.TempDir()
	if _, err := tracked(root); err == nil || !strings.Contains(err.Error(), "discovery of the tracked shell scripts failed") {
		t.Fatalf("%v", err)
	}
}
