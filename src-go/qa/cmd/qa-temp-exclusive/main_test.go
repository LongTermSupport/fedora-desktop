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
// rule and fixtures plus files. A machine without semgrep fails here with the gate's own message.
func realRun(t *testing.T, files map[string]string) (ghscopes.ExitCode, string, string) {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	for _, rel := range []string{".semgrep/temp-exclusive.yml", ".semgrep/temp-exclusive.fixture", ".semgrep/temp-exclusive.py", "scripts/qa-trees.tsv"} {
		files[rel] = projectFile(t, rel)
	}
	// The files inside .claude/ccy the gate still reads: its whole-tree run requires them.
	for _, rel := range ghscopes.TempExclusive.StillScannedFiles() {
		if _, given := files[rel]; !given {
			files[rel] = "nothing\n"
		}
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
	code := ghscopes.TempExclusive.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// A pid-named temporary moved into place, in shell and in Python, and the same writes through
// mktemp and mkstemp. The pid is joined so that this file is not itself a finding.
const (
	pid         = "$" + "$"
	shellPidTmp = "#!/usr/bin/env bash\ntmp=\"$file.tmp." + pid + "\"\nprintf x >\"$tmp\"\nmv -f -- \"$tmp\" \"$file\"\n"
	shellMktemp = "#!/usr/bin/env bash\ntmp=$(mktemp \"$file.XXXXXX\")\nprintf x >\"$tmp\"\nmv -f -- \"$tmp\" \"$file\"\n"
	pyPidTmp    = "import os\n\n\ndef put(path, text):\n    tmp = f\"{path}.tmp.{os.getpid()}\"\n    with open(tmp, \"w\") as handle:\n        handle.write(text)\n    os.replace(tmp, path)\n"
	pyMkstemp   = "import os\nimport tempfile\n\n\ndef put(path, text):\n    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))\n    with os.fdopen(fd, \"w\") as handle:\n        handle.write(text)\n    os.replace(tmp, path)\n"
)

func TestTheRealEngineFindsThePidTemporariesAndPassesTheExclusiveOnes(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{
		"scripts/bad.bash": shellPidTmp, "helpers/bad.py": pyPidTmp,
		"scripts/good.bash": shellMktemp, "helpers/good.py": pyMkstemp,
	})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "scripts/bad.bash:2  temp-not-exclusive") ||
		!strings.Contains(stdout, "helpers/bad.py:6  temp-not-exclusive") ||
		strings.Contains(stdout, "good.") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"scripts/good.bash": shellMktemp, "helpers/good.py": pyMkstemp})
	if code != ghscopes.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}
