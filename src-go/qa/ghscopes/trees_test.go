package ghscopes

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qatrees"
)

// The trees table is read and every file it keeps out of all gates is never handed to semgrep:
// a daemon-deployed tree, a single daemon-deployed file, and a shell-only tree's scripts, but
// not that tree's Python, which is ours.
func TestFilesTheTreesTableExcludesAreNeverHandedOver(t *testing.T) {
	w := newWorld(t, map[string]string{
		".claude/ccy/claude-supervise.py":  "x = 1\n",
		".claude/hooks/pre-tool-use":       "#!/bin/bash\n",
		".claude/hooks/handlers/h.py":      "x = 1\n",
		".claude/hooks/handlers/README.md": "# r\n",
		"CLAUDE/Plan/mkplan.bash":          "#!/usr/bin/env bash\n",
		"CLAUDE/Plan/other.bash":           "#!/usr/bin/env bash\n",
	})
	wantExit(t, w.main(), ExitOK)
	files := handed(w.run.scanArgv)
	for _, gone := range []string{".claude/ccy/claude-supervise.py", ".claude/hooks/pre-tool-use", ".claude/hooks/handlers/README.md", "CLAUDE/Plan/mkplan.bash"} {
		if slices.Contains(files, gone) {
			t.Errorf("%s was handed over: %v", gone, files)
		}
	}
	for _, kept := range []string{".claude/hooks/handlers/h.py", "CLAUDE/Plan/other.bash", "a.txt"} {
		if !slices.Contains(files, kept) {
			t.Errorf("%s was not handed over: %v", kept, files)
		}
	}
}

func TestAMissingOrBrokenTreesTableIsExitTwoAndNothingIsScanned(t *testing.T) {
	for name, text := range map[string]string{"missing": "", "no python row": "shell\tx\n"} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, map[string]string{qatrees.File: text})
			got := w.main()
			wantExit(t, got, ExitCannotRun)
			wantIn(t, "stderr", got.stderr, "ERROR:", qatrees.File)
			if w.run.scanArgv != nil {
				t.Fatalf("semgrep scanned %v", w.run.scanArgv)
			}
		})
	}
}

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
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin, ArgvLeak} {
		t.Run(spec.short, func(t *testing.T) {
			// The gate's own run: the scope it loads and the files it would hand to semgrep, over
			// the real git; a handed file the rule's excludes drop is one semgrep never opens.
			g := &gate{spec: spec, root: root, env: Env{Runner: proc.OS{}}}
			within, err := g.loadScope()
			if err != nil {
				t.Fatal(err)
			}
			handed, err := g.handOver(context.Background(), within)
			if err != nil {
				t.Fatal(err)
			}
			var opened []string
			for _, rel := range excluded {
				if slices.Contains(handed, rel) && !within.rule.matches(rel) {
					opened = append(opened, rel)
				}
			}
			if len(opened) > 0 {
				t.Errorf("qa-trees-drift: %s opens %d file(s) scripts/qa-trees.tsv keeps out of every gate (see CLAUDE/QA.md \"qa-trees-drift\"):\n%s", spec.short, len(opened), indent(opened))
			}
		})
	}
}
