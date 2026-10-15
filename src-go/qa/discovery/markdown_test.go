package discovery

import "testing"

// D18: the markdown list is the *.md files git would publish outside the shell trees; absolute.
func TestMarkdownListIsPublishedMarkdownOutsideTheExcludedTrees(t *testing.T) {
	w := newWorld(t)
	w.track("README.md", "")
	w.track("docs/a.md", "")
	w.track("docs/a.txt", "")
	w.track("untracked/x.md", "")
	w.track(".semgrep/x.md", "")
	w.fresh("docs/new.md", "")
	w.ignore("docs/ignored.md", "")
	wantPaths(t, w.main("markdown"), w.root+"/README.md", w.root+"/docs/a.md", w.root+"/docs/new.md")
}

// D18: whether a caller's documents are among them is the caller's judgement.
func TestAnEmptyMarkdownListIsAListNotAnError(t *testing.T) {
	w := newWorld(t)
	w.track("a.bash", "")
	got := w.main("markdown")
	wantExit(t, got, ExitOK)
	if got.stdout != "" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
}

// D19: every tracked path, repository relative, nothing excluded, the deleted dropped.
func TestTrackedFilesListsEveryTrackedPathLessTheDeleted(t *testing.T) {
	w := newWorld(t)
	w.track("b.txt", "")
	w.track("untracked/x", "")
	w.track("gone.md", "")
	w.deleted = []string{"gone.md"}
	w.fresh("new.md", "")
	wantPaths(t, w.main("tracked-files"), "b.txt", "untracked/x")
}

// D19, D13: a repository that tracks nothing is a broken discovery.
func TestAnEmptyTrackedFilesListIsABrokenDiscovery(t *testing.T) {
	w := newWorld(t)
	w.fresh("new.md", "")
	got := w.main("tracked-files")
	wantExit(t, got, ExitCannotRun)
	wantStderr(t, got, "ERROR: found 0 tracked files under "+w.root)
}
