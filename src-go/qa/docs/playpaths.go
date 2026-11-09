package docs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// playPath is a repository playbook path as a command or a document writes it. A path built by
// expansion or written as a pattern ($name, {{ var }}, *, <name>) stops the match before .yml, so
// it is never one; an elided path (playbooks/...yml) is skipped by isElided.
var playPath = regexp.MustCompile(`playbooks/[A-Za-z0-9_./-]*\.yml`)

// playPathTrees are the tracked trees, besides the documents, whose text tells someone which
// playbook to run: the installer, the scripts and every deployed file.
var playPathTrees = []string{"scripts/", "files/"}

// playPathFiles are the single tracked files of the same kind.
var playPathFiles = []string{"run.bash"}

// isPlayPathSource is whether a tracked path is one whose playbook paths are checked. A test
// suite or its fixture names plays it builds in a temporary tree, so it is left out.
func isPlayPathSource(rel string) bool {
	if slices.Contains(playPathFiles, rel) {
		return true
	}
	base := path.Base(rel)
	if strings.HasPrefix(base, "test-") || strings.HasPrefix(base, "test_") || strings.Contains(rel, "/testdata/") {
		return false
	}
	for _, tree := range playPathTrees {
		if strings.HasPrefix(rel, tree) {
			return true
		}
	}
	return false
}

// isElided is whether a matched path is written with an ellipsis, as an example and not a file.
func isElided(p string) bool { return strings.Contains(p, "...") }

// isRegular is whether a tracked path is a file of its own: a tracked symlink (into a vendored
// tree, say) is another file's text, judged where it lives if at all.
func (g *gate) isRegular(rel string) (bool, error) {
	info, err := g.env.Lstat(filepath.Join(g.root, filepath.FromSlash(rel)))
	if err != nil {
		if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
			err = pathErr.Err
		}
		return false, fmt.Errorf("cannot stat %s: %w", rel, err)
	}
	return info.Mode().IsRegular(), nil
}

// checkPlayPaths requires every playbook path written in a document, the installer, a script or
// a deployed file to be a playbook the repository tracks: a command naming a play that has moved
// sends its reader to a file that is not there.
func (g *gate) checkPlayPaths(population []string, tracked trackedPaths) ([]finding, error) {
	sources := slices.Clone(population)
	for rel := range tracked.files {
		if isPlayPathSource(rel) && !slices.Contains(sources, rel) {
			sources = append(sources, rel)
		}
	}
	slices.Sort(sources)
	findings := []finding{}
	for _, rel := range sources {
		regular, err := g.isRegular(rel)
		if err != nil {
			return nil, err
		}
		if !regular {
			continue
		}
		body, err := g.read(rel)
		if err != nil {
			return nil, err
		}
		findings = append(findings, missingPlays(rel, body, tracked)...)
	}
	return findings, nil
}

// missingPlays are the playbook paths of one file's text that name no tracked playbook; a binary
// file has none.
func missingPlays(rel string, body []byte, tracked trackedPaths) []finding {
	if bytes.IndexByte(body, 0) >= 0 {
		return nil
	}
	var findings []finding
	for n, line := range strings.Split(string(body), "\n") {
		for _, p := range playPath.FindAllString(line, -1) {
			if !isElided(p) && !tracked.files[path.Clean(p)] {
				findings = append(findings, finding{File: rel, Line: n + 1, Target: p, Problem: "names a playbook the repository does not have"})
			}
		}
	}
	return findings
}
