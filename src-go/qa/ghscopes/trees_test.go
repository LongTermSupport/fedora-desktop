package ghscopes

import (
	"bytes"
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
// this engine leaves out every published file scripts/qa-trees.tsv keeps out of all gates, but
// the rows its spec still scans, which it must then really open; the
// daemon-deployed files above all, which the owner ruled are never gated here. A gate that
// keeps a second list of trees that are not ours drifts from the table; this is the proof that
// it has not.
func TestEveryGateLeavesOutWhatTheTreesTableExcludes(t *testing.T) {
	root, excluded := tableExcluded(t)
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin, ArgvLeak, PlaceIssuedToken, BootHold, SecretScreen} {
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
			opened, stillScanned := openedOf(excluded, handed, within.rule, spec.stillScans)
			if len(spec.stillScans) > 0 && len(stillScanned) == 0 {
				t.Errorf("%s declares it still scans %v but opens nothing there", spec.short, spec.stillScans)
			}
			if len(opened) > 0 {
				t.Errorf("qa-trees-drift: %s opens %d file(s) scripts/qa-trees.tsv keeps out of every gate (see CLAUDE/QA.md \"qa-trees-drift\"):\n%s", spec.short, len(opened), indent(opened))
			}
		})
	}
}

// openedOf splits the table-excluded files the gate hands to semgrep and its rule does not drop:
// those in a row the spec still scans, and the rest, which are drift.
func openedOf(excluded, handed []string, rule excludes, stillScans []string) ([]string, []string) {
	var opened, stillScanned []string
	for _, rel := range excluded {
		switch {
		case !slices.Contains(handed, rel) || rule.matches(rel):
		case inRow(rel, stillScans):
			stillScanned = append(stillScanned, rel)
		default:
			opened = append(opened, rel)
		}
	}
	return opened, stillScanned
}

// inRow is whether rel lies in one of the table rows dirs, matched as the table matches: whole
// segments anywhere in the path.
func inRow(rel string, dirs []string) bool {
	return slices.ContainsFunc(dirs, func(dir string) bool { return strings.Contains("/"+rel+"/", "/"+dir+"/") })
}

// The exception to the table is the semgrep tree and nothing else (owner ruling Q18 of issue
// #217 pending): a daemon-deployed or vendored row added here would reopen what the ruling
// keeps out, so it fails this test rather than pass the drift test above.
func TestOnlySemgrepIsStillScanned(t *testing.T) {
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin, ArgvLeak, PlaceIssuedToken, BootHold, SecretScreen} {
		for _, dir := range spec.stillScans {
			if dir != ".semgrep" {
				t.Errorf("%s still scans %q; only .semgrep may be an exception to scripts/qa-trees.tsv", spec.short, dir)
			}
		}
	}
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin} {
		if !slices.Equal(spec.stillScans, []string{".semgrep"}) {
			t.Errorf("%s still scans %v, want [.semgrep]: the coverage it had before the table governed it", spec.short, spec.stillScans)
		}
	}
}

// An exception naming a row the table does not have is exit 2, and nothing is scanned.
func TestAStillScannedRowTheTableLacksIsExitTwo(t *testing.T) {
	w := newWorld(t, nil)
	spec := GHScopes
	spec.stillScans = []string{"gone"}
	var stdout, stderr bytes.Buffer
	code := spec.Main(nil, Env{Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	got := result{code, stdout.String(), stderr.String()}
	wantExit(t, got, ExitCannotRun)
	wantIn(t, "stderr", got.stderr, "ERROR:", "gone")
	if w.run.scanArgv != nil {
		t.Fatalf("semgrep scanned %v", w.run.scanArgv)
	}
}
