package docs

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// finding is one thing wrong: where, what was written, and why it is wrong. A finding of the
// catalogue or the index has line 0.
type finding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Target  string `json:"target"`
	Problem string `json:"problem"`
}

// vendoredRoots are the trees holding a DIFFERENT repository vendored into this checkout. A link
// landing inside one is never a finding: the file is not ours, a broken link there could not be
// fixed here, and CI has no copy of it at all, so the verdict must not depend on what is
// installed. They are declared rather than detected for that reason, and each is also outside
// the population (the shared trees table), or this gate would read another repository's
// Markdown as its own.
var vendoredRoots = []string{".claude/hooks-daemon", "roles/vendor"}

// vendoredCounts are the three outcomes of a link into a vendored repository.
type vendoredCounts struct {
	OK           int       `json:"ok"`
	Unverifiable int       `json:"unverifiable"`
	Broken       []finding `json:"broken"`
}

var scheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// checkLinks checks every link of every document in the population.
func (g *gate) checkLinks(population []string, tracked trackedPaths) ([]finding, vendoredCounts, error) {
	findings := []finding{}
	vendored := vendoredCounts{Broken: []finding{}}
	for _, rel := range population {
		doc, err := g.document(rel)
		if err != nil {
			return nil, vendored, err
		}
		for _, l := range doc.links {
			problem, class, err := g.judge(rel, l.target, tracked)
			if err != nil {
				return nil, vendored, err
			}
			f := finding{File: rel, Line: l.line, Target: l.target, Problem: problem}
			switch class {
			case classFinding:
				findings = append(findings, f)
			case classVendoredOK:
				vendored.OK++
			case classVendoredAbsent:
				vendored.Unverifiable++
			case classVendoredBroken:
				vendored.Broken = append(vendored.Broken, f)
			case classClean:
			}
		}
	}
	sortFindings(findings)
	sortFindings(vendored.Broken)
	return findings, vendored, nil
}

// sortFindings orders findings by file then line, keeping the order they were found in for a tie.
func sortFindings(list []finding) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].File != list[j].File {
			return list[i].File < list[j].File
		}
		return list[i].Line < list[j].Line
	})
}

// outcome is how one link came out.
type outcome int

const (
	classClean outcome = iota
	classFinding
	classVendoredOK
	classVendoredAbsent
	classVendoredBroken
)

// judge decides one link of the document at docRel. The problem is set for a finding and for a
// broken vendored link.
func (g *gate) judge(docRel, target string, tracked trackedPaths) (string, outcome, error) {
	if scheme.MatchString(target) || strings.HasPrefix(target, "//") {
		return "", classClean, nil
	}
	if strings.HasPrefix(target, "/") {
		return "link is an absolute path: use a path relative to this document", classFinding, nil
	}
	filePart, anchor, hasAnchor := strings.Cut(target, "#")
	filePart, filePartOK := decode(filePart)
	anchor, anchorOK := decode(anchor)
	if !filePartOK || !anchorOK {
		return "link target is not valid percent-encoding", classFinding, nil
	}
	targetRel := docRel
	if filePart != "" {
		abs := filepath.Join(g.root, filepath.FromSlash(path.Dir(docRel)), filepath.FromSlash(filePart))
		rel, problem, class, err := g.judgePath(abs, tracked)
		if err != nil || class != classClean {
			return problem, class, err
		}
		targetRel = rel
	}
	if !hasAnchor || anchor == "" || !strings.HasSuffix(targetRel, ".md") {
		return "", classClean, nil
	}
	doc, err := g.document(targetRel)
	if err != nil {
		return "", classClean, err
	}
	if !doc.headings[anchor] {
		return "no heading matches the anchor", classFinding, nil
	}
	return "", classClean, nil
}

// decode is the text with its percent-encoding resolved, and whether it was valid.
func decode(s string) (string, bool) {
	decoded, err := url.PathUnescape(s)
	return decoded, err == nil
}

// judgePath judges the file a link names. On a clean result the first value is the target
// relative to the root, whose anchor the caller goes on to check.
func (g *gate) judgePath(abs string, tracked trackedPaths) (string, string, outcome, error) {
	targetRel, inside := g.relative(abs)
	if root := vendoredRootOf(targetRel, inside); root != "" {
		class, problem, err := g.vendoredOutcome(root, abs)
		return "", problem, class, err
	}
	exists, err := g.present(abs)
	if err != nil {
		return "", "", classClean, err
	}
	if !exists {
		return "", "target does not exist", classFinding, nil
	}
	if !inside || !tracked.owns(targetRel) {
		problem := "target is not tracked by this repository"
		if nested := g.nestedRepository(targetRel, inside); nested != "" {
			problem += fmt.Sprintf(" — a repository is nested at %s; if it is vendored, declare it in the vendored roots (src-go/qa/docs)", nested)
		}
		return "", problem, classFinding, nil
	}
	return targetRel, "", classClean, nil
}

// relative is the path under the root, and whether it is under it at all.
func (g *gate) relative(abs string) (string, bool) {
	rel, err := filepath.Rel(g.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// vendoredRootOf is the declared vendored tree holding the path: the root itself or anything
// under it. A sibling that merely begins with the same letters is not.
func vendoredRootOf(rel string, inside bool) string {
	if !inside {
		return ""
	}
	for _, root := range vendoredRoots {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return root
		}
	}
	return ""
}

// vendoredOutcome counts a link into a vendored repository: verified when the repository is here
// and has the target, unverifiable when the repository is absent (CI's case), broken when it is
// here and does not.
func (g *gate) vendoredOutcome(root, abs string) (outcome, string, error) {
	rootPresent, err := g.present(filepath.Join(g.root, filepath.FromSlash(root)))
	if err != nil {
		return classClean, "", err
	}
	if !rootPresent {
		return classVendoredAbsent, "", nil
	}
	targetPresent, err := g.present(abs)
	if err != nil {
		return classClean, "", err
	}
	if targetPresent {
		return classVendoredOK, "", nil
	}
	return classVendoredBroken, fmt.Sprintf("broken link into the vendored repository at %s, which IS present here — it has probably moved the file", root), nil
}

// present is whether the path exists, a symlink being followed. A path that cannot exist
// (something along it is a file, or the name is too long) is absent; any other failure to look
// is an error.
func (g *gate) present(abs string) (bool, error) {
	_, err := g.env.Stat(abs)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ENAMETOOLONG):
		return false, nil
	}
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		err = pathErr.Err
	}
	return false, fmt.Errorf("cannot look for a link target: %w", err)
}

// nestedRepository is the nearest directory at or above the target, below the root, that holds
// a .git (a file, for a linked worktree). A hint on a target that is already a finding, not a
// verdict; "" when there is none or the target is outside the root.
func (g *gate) nestedRepository(rel string, inside bool) string {
	if !inside {
		return ""
	}
	for dir := rel; dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
		if _, err := g.env.Lstat(filepath.Join(g.root, filepath.FromSlash(dir), ".git")); err == nil {
			return dir
		}
	}
	return ""
}
