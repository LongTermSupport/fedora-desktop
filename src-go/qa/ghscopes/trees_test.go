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
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin, ArgvLeak, PlaceIssuedToken, BootHold, GHPoll, SecretScreen} {
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
			opened, stillScanned := openedOf(excluded, handed, within.rule, spec.stillScans, spec.stillScansFiles)
			wantStillScanned(t, spec, stillScanned)
			if len(opened) > 0 {
				t.Errorf("qa-trees-drift: %s opens %d file(s) scripts/qa-trees.tsv keeps out of every gate (see CLAUDE/QA.md \"qa-trees-drift\"):\n%s", spec.short, len(opened), indent(opened))
			}
		})
	}
}

// wantStillScanned is that the gate really opens something in each row it still scans and every
// file it still scans: an exception that reads nothing is a stale one.
func wantStillScanned(t *testing.T, spec Spec, stillScanned []string) {
	t.Helper()
	if len(spec.stillScans) > 0 && !slices.ContainsFunc(stillScanned, func(rel string) bool { return inRow(rel, spec.stillScans) }) {
		t.Errorf("%s declares it still scans %v but opens nothing there", spec.short, spec.stillScans)
	}
	for _, rel := range spec.stillScansFiles {
		if !slices.Contains(stillScanned, rel) {
			t.Errorf("%s declares it still scans %s but does not open it", spec.short, rel)
		}
	}
}

// openedOf splits the table-excluded files the gate hands to semgrep and its rule does not drop:
// those in a row or among the files the spec still scans, and the rest, which are drift.
func openedOf(excluded, handed []string, rule excludes, stillScans, stillScansFiles []string) ([]string, []string) {
	var opened, stillScanned []string
	for _, rel := range excluded {
		switch {
		case !slices.Contains(handed, rel) || rule.Matches(rel):
		case inRow(rel, stillScans) || slices.Contains(stillScansFiles, rel):
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
	for _, spec := range []Spec{GHScopes, FindRace, ArchPin, ArgvLeak, PlaceIssuedToken, BootHold, GHPoll, SecretScreen} {
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

// The one file a gate still reads inside a tree the table keeps out is this project's own image
// Dockerfile, for arch-pin alone (issue #408: Plan 00243 fixed arch-pinned-artefact findings in
// it, and the .claude/ccy row took them out of the gate). The rest of .claude/ccy is the CCY
// runtime tree and the daemon-deployed claude-supervise, which the owner ruled out of every gate,
// so a second file named here fails this test rather than pass the drift test above.
func TestOnlyTheCCYDockerfileIsAStillScannedFile(t *testing.T) {
	for _, spec := range []Spec{GHScopes, FindRace, ArgvLeak, PlaceIssuedToken, BootHold, GHPoll, SecretScreen} {
		if len(spec.stillScansFiles) > 0 {
			t.Errorf("%s still scans the files %v; only arch-pin may name a file inside an excluded tree", spec.short, spec.stillScansFiles)
		}
	}
	if !slices.Equal(ArchPin.stillScansFiles, []string{".claude/ccy/Dockerfile"}) {
		t.Errorf("arch-pin still scans the files %v, want [.claude/ccy/Dockerfile]", ArchPin.stillScansFiles)
	}
}

// stillScansWorld is a world with a file the table keeps out that a spec names all the same,
// beside one in the same tree that it does not name.
func stillScansWorld(t *testing.T) *world {
	t.Helper()
	return newWorld(t, map[string]string{
		".claude/ccy/Dockerfile":          "FROM x\n",
		".claude/ccy/claude-supervise.py": "x = 1\n",
	})
}

// runSpec runs spec over the world with args.
func runSpec(w *world, spec Spec, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := spec.Main(args, Env{Stdout: &stdout, Stderr: &stderr, Cwd: w.root, Runner: w.run})
	return result{code, stdout.String(), stderr.String()}
}

// A still-scanned file is handed to semgrep on the whole-tree run and on a run that names it,
// and the gate's Reader says it reads it (qa-changed plans the gate for it); the rest of its tree
// stays out.
func TestAStillScannedFileIsReadAndTheRestOfItsTreeIsNot(t *testing.T) {
	spec := GHScopes
	spec.stillScansFiles = []string{".claude/ccy/Dockerfile"}

	w := stillScansWorld(t)
	wantExit(t, runSpec(w, spec), ExitOK)
	files := handed(w.run.scanArgv)
	if !slices.Contains(files, ".claude/ccy/Dockerfile") {
		t.Errorf("the still-scanned file was not handed over: %v", files)
	}
	if slices.Contains(files, ".claude/ccy/claude-supervise.py") {
		t.Errorf("the rest of its tree was handed over: %v", files)
	}

	w = stillScansWorld(t)
	wantExit(t, runSpec(w, spec, "--", ".claude/ccy/Dockerfile", ".claude/ccy/claude-supervise.py"), ExitOK)
	if files := handed(w.run.scanArgv); !slices.Equal(files, []string{".claude/ccy/Dockerfile"}) {
		t.Errorf("a named run handed %v, want only the still-scanned file", files)
	}

	reader, err := spec.Reader(w.root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{".claude/ccy/Dockerfile": true, ".claude/ccy/claude-supervise.py": false} {
		got, readErr := reader.Reads(rel)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got != want {
			t.Errorf("Reader.Reads(%s) = %v, want %v", rel, got, want)
		}
	}
}

// A still-scanned file that is not inside a tree the table keeps out (a stale exception), that
// is not on disk, or that git would not publish is exit 2, and nothing is scanned.
func TestABadStillScannedFileIsExitTwo(t *testing.T) {
	for name, tc := range map[string]struct {
		file     string
		unlisted bool
	}{
		"not excluded": {file: "a.txt"},
		"missing":      {file: ".claude/ccy/gone"},
		"unpublished":  {file: ".claude/ccy/Dockerfile", unlisted: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := stillScansWorld(t)
			if tc.unlisted {
				w.run.listed = slices.DeleteFunc(w.run.listed, func(rel string) bool { return rel == tc.file })
			}
			spec := GHScopes
			spec.stillScansFiles = []string{tc.file}
			got := runSpec(w, spec)
			wantExit(t, got, ExitCannotRun)
			wantIn(t, "stderr", got.stderr, "ERROR:", tc.file)
			if w.run.scanArgv != nil {
				t.Fatalf("semgrep scanned %v", w.run.scanArgv)
			}
		})
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
