package docs

import (
	"errors"
	"path"
	"slices"
	"strings"
)

// excludedPrefixes are the trees this gate leaves out beyond those the shared trees table
// excludes: the plans (a core gate must not depend on plan content) and the agent definitions.
var excludedPrefixes = []string{"CLAUDE/Plan/", ".claude/agents/"}

// inScope is whether this gate owns a repository-relative Markdown path: the top-level readme
// and index, the documentation tree, the path-triggered rules, every topic file, and any
// directory's own CLAUDE.md.
func inScope(rel string) bool {
	if !strings.HasSuffix(rel, ".md") {
		return false
	}
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return false
		}
	}
	switch {
	case rel == "README.md", rel == "CLAUDE.md":
		return true
	case strings.HasPrefix(rel, "docs/"), strings.HasPrefix(rel, ".claude/rules/"), strings.HasPrefix(rel, "CLAUDE/"):
		return true
	}
	return path.Base(rel) == "CLAUDE.md"
}

// population is the documents the gate scans: the Markdown files git would publish, outside the
// excluded trees, that are in scope. None is an error: a pass over nothing proves nothing.
func (g *gate) population() ([]string, error) {
	published, err := g.list("markdown", true)
	if err != nil {
		return nil, err
	}
	var scoped []string
	for _, rel := range published {
		if inScope(rel) {
			scoped = append(scoped, rel)
		}
	}
	if len(scoped) == 0 {
		return nil, errors.New("found 0 in-scope markdown files: a zero-file scan means discovery is broken, not that the docs are clean")
	}
	slices.Sort(scoped)
	return scoped, nil
}

// trackedPaths is every path the repository tracks and the directories holding them.
type trackedPaths struct {
	files map[string]bool
	dirs  map[string]bool
}

// trackedSet reads what the repository tracks. The question a link target is asked is
// trackedness, not ignoredness: a file on disk that nobody added passes on this machine and fails
// in a clean checkout.
func (g *gate) trackedSet() (trackedPaths, error) {
	files, err := g.list("tracked-files", false)
	if err != nil {
		return trackedPaths{}, err
	}
	// The repository itself is never listed and is obviously ours.
	tracked := trackedPaths{files: map[string]bool{}, dirs: map[string]bool{".": true}}
	for _, rel := range files {
		tracked.files[rel] = true
		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			tracked.dirs[dir] = true
		}
	}
	return tracked, nil
}

// owns is whether the repository tracks the path, as a file or as a directory holding one.
func (t trackedPaths) owns(rel string) bool { return t.files[rel] || t.dirs[rel] }

// trackedCount is how many of the scanned documents the repository tracks.
func (t trackedPaths) trackedCount(population []string) int {
	n := 0
	for _, rel := range population {
		if t.files[rel] {
			n++
		}
	}
	return n
}
