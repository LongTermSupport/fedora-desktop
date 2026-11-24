package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
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
	files[".semgrep/gh-poll.yml"] = projectFile(t, ".semgrep/gh-poll.yml")
	files[".semgrep/gh-poll.fixture"] = projectFile(t, ".semgrep/gh-poll.fixture")
	files["scripts/qa-trees.tsv"] = projectFile(t, "scripts/qa-trees.tsv")
	// The files inside .claude/ccy the gate still reads: its whole-tree run requires them.
	for _, rel := range ghscopes.GHPoll.StillScannedFiles() {
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
	code := ghscopes.GHPoll.Main([]string{"--root", root}, ghscopes.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}})
	return code, stdout.String(), stderr.String()
}

// A watcher at gh's 3-second default, in a script and in a recipe, and the same watcher once a
// minute. The command is joined so that this file is not itself a finding.
const (
	watch      = "gh run " + "watch"
	fastScript = "#!/usr/bin/env bash\n" + watch + " \"$id\" --exit-status\n"
	fastRecipe = "1. Push.\n2. `" + watch + "` until it is green.\n"
	slowScript = "#!/usr/bin/env bash\n" + watch + " \"$id\" --exit-status --interval 60\n"
)

func TestTheRealEngineFindsTheFastWatchersAndPassesTheSlowOne(t *testing.T) {
	code, stdout, stderr := realRun(t, map[string]string{
		"scripts/fast.bash": fastScript, "docs/recipe.md": fastRecipe, "scripts/slow.bash": slowScript,
	})
	if code != ghscopes.ExitFailed || !strings.Contains(stdout, "scripts/fast.bash:2  github-poll-too-often") ||
		!strings.Contains(stdout, "docs/recipe.md:2  github-poll-too-often") || strings.Contains(stdout, "scripts/slow.bash") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = realRun(t, map[string]string{"scripts/slow.bash": slowScript})
	if code != ghscopes.ExitOK {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// This project's own files inside .claude/ccy are read although scripts/qa-trees.tsv keeps the
// rest of that tree out (issues #408 and #412): a finding in each is reported, and the
// daemon-deployed supervisor's is not.
func TestTheRealEngineReadsTheCCYProjectFiles(t *testing.T) {
	ccy := ghscopes.GHPoll.StillScannedFiles()
	if !slices.Contains(ccy, ".claude/ccy/ccy.env") {
		t.Fatalf("the gate does not still scan the ccy config: %v", ccy)
	}
	files := map[string]string{".claude/ccy/claude-supervise.py": fastScript}
	for _, rel := range ccy {
		files[rel] = fastScript
	}
	code, stdout, stderr := realRun(t, files)
	if code != ghscopes.ExitFailed || strings.Contains(stdout, "claude-supervise") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, rel := range ccy {
		if !strings.Contains(stdout, rel+":2  github-poll-too-often") {
			t.Errorf("%s was not read:\n%s", rel, stdout)
		}
	}
}
