package ghscopes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
)

// tableExcluded is every file git tracks in this repository that scripts/qa-trees.tsv keeps out
// of every gate, and the repository root.
func tableExcluded(t *testing.T) (string, []string) {
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
	var excluded []string
	for rel := range strings.SplitSeq(res.Stdout, "\x00") {
		if rel == "" {
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(root, rel)); statErr != nil {
			continue
		}
		out, exErr := trees.ExcludesFile(repo, rel)
		if exErr != nil {
			t.Fatal(exErr)
		}
		if out {
			excluded = append(excluded, rel)
		}
	}
	if len(excluded) == 0 {
		t.Fatal("the trees table excludes no published file; the check would prove nothing")
	}
	return root, excluded
}

// qa-trees-drift (issue #217, CLAUDE/QA.md "qa-trees-drift"): on this repository, every gate of
// this engine leaves out every published file scripts/qa-trees.tsv keeps out of all gates; the
// daemon-deployed files above all, which the owner ruled are never gated here. A gate that
// keeps a second list of trees that are not ours drifts from the table; this is the proof that
// it has not.
func TestEveryGateLeavesOutWhatTheTreesTableExcludes(t *testing.T) {
	root, excluded := tableExcluded(t)
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin} {
		t.Run(spec.short, func(t *testing.T) {
			s, err := (&gate{spec: spec, root: root}).loadScope()
			if err != nil {
				t.Fatal(err)
			}
			var opened []string
			for _, rel := range excluded {
				if !s.leavesOut(rel) {
					opened = append(opened, rel)
				}
			}
			if len(opened) > 0 {
				t.Errorf("qa-trees-drift: %s opens %d file(s) scripts/qa-trees.tsv keeps out of every gate (see CLAUDE/QA.md \"qa-trees-drift\"):\n%s", spec.short, len(opened), indent(opened))
			}
		})
	}
}
