package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/teamchatseparate"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(teamchatseparate.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(teamchatseparate.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root that is not a git work tree is a run that could not judge (K6).
	if code := run([]string{"--root", t.TempDir()}); code != int(teamchatseparate.ExitCannotRun) {
		t.Fatalf("not a work tree: exit %d", code)
	}
}

// gitRepo makes a git work tree with a .gitignore of the given lines and the given files.
func gitRepo(t *testing.T, ignore string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "init", "-q"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git init: %v %+v", err, res)
	}
	files[".gitignore"] = ignore
	for rel, text := range files {
		write(t, root, rel, text)
	}
	return root
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// K4: the files git would publish under the scan paths: untracked-not-ignored included, ignored
// left out, files elsewhere left out.
func TestPublishedListsWhatGitWouldPublishUnderTheScanPaths(t *testing.T) {
	root := gitRepo(t, "secret.go\n", map[string]string{
		"src-go/internal/teamchat/paths/paths.go":  "",
		"src-go/internal/teamchat/paths/secret.go": "",
		"playbooks/imports/play-teamchat.yml":      "",
		"docs/teamchat.md":                         "",
	})
	got, err := published(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{"playbooks/imports/play-teamchat.yml", "src-go/internal/teamchat/paths/paths.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

// End to end on a real work tree with the shipped data: a clean v2 tree passes, a v1 path in
// an untracked v2 play fails it, and the same text in an ignored play does not.
func TestEndToEndOnARealWorkTree(t *testing.T) {
	names, err := os.ReadFile("../../teamchatseparate/v1-names.txt")
	if err != nil {
		t.Fatal(err)
	}
	root := gitRepo(t, "playbooks/imports/play-teamchat-ignored.yml\n", map[string]string{
		"src-go/qa/teamchatseparate/v1-names.txt": string(names),
		"src-go/qa/teamchatseparate/allowed.tsv":  "# none\n",
		"src-go/internal/teamchat/paths/paths.go": "package paths\n",
	})
	if code := run([]string{"--root", root}); code != int(teamchatseparate.ExitOK) {
		t.Fatalf("clean tree: exit %d", code)
	}
	write(t, root, "playbooks/imports/play-teamchat-ignored.yml", "state: /var/lib/agent-bus\n")
	if code := run([]string{"--root", root}); code != int(teamchatseparate.ExitOK) {
		t.Fatalf("an ignored file was judged: exit %d", code)
	}
	write(t, root, "playbooks/imports/play-teamchat.yml", "state: /var/lib/agent-bus\n")
	if code := run([]string{"--root", root}); code != int(teamchatseparate.ExitFailed) {
		t.Fatalf("a v1 path in a v2 play passed: exit %d", code)
	}
}
