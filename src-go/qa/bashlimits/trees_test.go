package bashlimits

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
)

// excludedBash is every bash file git tracks in this repository that scripts/qa-trees.tsv keeps
// out of every gate.
func excludedBash(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo := os.DirFS(root)
	trees, err := qatrees.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{Argv: []string{"git", "ls-files", "-z", "--cached"}, Dir: root})
	if err != nil || res.Code != 0 {
		t.Fatalf("git ls-files: %v %+v", err, res)
	}
	var bash []string
	for rel := range strings.SplitSeq(res.Stdout, "\x00") {
		if isExcludedBash(t, repo, trees, rel) {
			bash = append(bash, rel)
		}
	}
	if len(bash) == 0 {
		t.Fatal("the trees table excludes no bash file; the check would prove nothing")
	}
	return bash
}

func isExcludedBash(t *testing.T, repo fs.FS, trees qatrees.Table, rel string) bool {
	t.Helper()
	if rel == "" {
		return false
	}
	if info, err := fs.Stat(repo, rel); err != nil || !info.Mode().IsRegular() {
		return false
	}
	out, err := trees.ExcludesFile(repo, rel)
	if err != nil {
		t.Fatal(err)
	}
	if !out {
		return false
	}
	bash, err := IsCandidate(repo, rel)
	if err != nil {
		t.Fatal(err)
	}
	return bash
}

// qa-trees-drift (issue #217, CLAUDE/QA.md "qa-trees-drift"): on this repository, every bash
// file scripts/qa-trees.tsv keeps out of every gate is one this gate does not judge. Its own
// exemptions (Exemptions, DaemonOwnedFiles) are a second list of trees that are not ours, kept
// because a daemon-owned single file must be proven shipped by an upgrade; this is the proof
// that the second list has not drifted from the table.
func TestEveryBashFileTheTreesTableExcludesIsUnjudged(t *testing.T) {
	var judged []string
	for _, rel := range excludedBash(t) {
		unjudged, err := IsUnjudged(rel)
		if err != nil {
			t.Fatal(err)
		}
		if !unjudged {
			judged = append(judged, rel)
		}
	}
	if len(judged) > 0 {
		t.Errorf("qa-trees-drift: bash-limits judges %d bash file(s) scripts/qa-trees.tsv keeps out of every gate (see CLAUDE/QA.md \"qa-trees-drift\"):\n    %s", len(judged), strings.Join(judged, "\n    "))
	}
}
