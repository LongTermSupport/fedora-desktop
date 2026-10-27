package changed

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// A suite reaches repository files it never names: cp "$REPO_ROOT"/helpers/self_update/*.py
// names no file under it, and cp -r "$ROOT/files/etc/skel" none in the tree. suiteReaches reads
// those paths so that a change a glob matches, or one inside a directory a suite reaches, selects
// the suite as a change it names does.
//
// It reads the text, not the shell: a variable is rooted in the repository by a top-level
// assignment of one of the forms the suites use (the script's own directory through
// dirname "${BASH_SOURCE[0]}" or dirname "$0"; cd "<path>" && pwd; dirname "<path>"; another
// rooted variable with a path after it). An indented assignment is ignored (it is a function's
// or a command's own), and a top-level one to anything else unroots the name. A reach is a
// rooted variable with a literal path after it; the path stops at the first character that is
// not a path character, so "$ROOT/x/$name" reaches x. The definition of a rooted variable is
// not a reach; its uses are.

var (
	assignment = regexp.MustCompile(`^(?:export |readonly |declare -r )?([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	ownDir     = regexp.MustCompile(`\$\(dirname \$\{?(?:BASH_SOURCE\[0\]|0)\}?\)`)
	dirnameOf  = regexp.MustCompile(`\$\(dirname (\$\{?[A-Za-z0-9_]+\}?[^ )]*)\)`)
	cdPwd      = regexp.MustCompile(`^\$\(cd (\S+) && pwd(?: -P)?\)$`)
	rootedPath = regexp.MustCompile(`^\$\{?([A-Za-z0-9_]+)\}?(/[A-Za-z0-9_.@+,*?/-]*)?$`)
	reference  = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?((?:"?/[A-Za-z0-9_.@+,*?/-]*)*)`)
	// treeCommand is a command that takes in a whole tree: a recursive copy, a sync, an archive,
	// a link, a walk or a recursive search.
	treeCommand = regexp.MustCompile(`(?:^|[\s;&|(])(?:cp -[A-Za-z]*[rRa]|rsync|tar|ln -[A-Za-z]*s|find|grep -[A-Za-z]*[rR])(?:\s|$)`)
)

// suiteDirVar is the name the script's own directory is bound to while an assignment is read;
// no shell variable can have it.
const suiteDirVar = "0suite"

// suiteReaches is the repository paths (repo-relative; globs as path.Match reads them) the suite
// at path suite reaches through a variable rooted in the repository, in the order they appear.
func suiteReaches(text, suite string) []string {
	rooted := map[string]string{suiteDirVar: path.Dir(suite)}
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := assignment.FindStringSubmatch(line); m != nil {
			if p, ok := resolveValue(m[2], rooted); ok {
				rooted[m[1]] = p
				continue
			}
			delete(rooted, m[1])
		}
		for _, at := range reference.FindAllStringSubmatchIndex(line, -1) {
			if p, ok := reachAt(line, at, rooted); ok && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// reachAt is the reach of the reference match at in line. An escaped \$NAME is text the suite
// looks for, not a path it uses. The variable alone is a reach on a line that copies, links or
// walks a tree (cp -R "$HOOK_DIR" …), and otherwise not: "$LIB_DIR/$f" reaches only what $f
// names. The repository root and a path that leaves it are no reach; and nor is one under
// src-go/, whose changes select the suites of the commands whose build closure holds them
// (buildOwners), not every suite that builds in there.
func reachAt(line string, at []int, rooted map[string]string) (string, bool) {
	if at[0] > 0 && line[at[0]-1] == '\\' {
		return "", false
	}
	base, ok := rooted[line[at[2]:at[3]]]
	if !ok {
		return "", false
	}
	p, ok := inRepo(path.Join(base, strings.ReplaceAll(line[at[4]:at[5]], `"`, "")))
	bare := at[4] == at[5]
	if !ok || p == "." || p == "src-go" || strings.HasPrefix(p, "src-go/") || p == base && (!bare || !treeCommand.MatchString(line)) {
		return "", false
	}
	return p, true
}

// resolveValue is the repo-relative path an assignment's value holds, when it is one of the
// rooted forms.
func resolveValue(raw string, rooted map[string]string) (string, bool) {
	if strings.HasPrefix(raw, "'") {
		return "", false
	}
	v := strings.ReplaceAll(strings.TrimSpace(raw), `"`, "")
	v = ownDir.ReplaceAllString(v, "$${"+suiteDirVar+"}")
	v = dirnameOf.ReplaceAllString(v, "$1/..")
	if m := cdPwd.FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	m := rootedPath.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	base, ok := rooted[m[1]]
	if !ok {
		return "", false
	}
	return inRepo(path.Join(base, m[2]))
}

// inRepo is p when it does not leave the repository.
func inRepo(p string) (string, bool) {
	return p, p != ".." && !strings.HasPrefix(p, "../")
}

// reached is whether a suite's reach takes in the changed file: the pattern matches the file, or
// a directory that holds it (a suite that copies or walks a tree reaches every file in it).
func reached(pattern, rel string) bool {
	for p := rel; p != "." && p != "/"; p = path.Dir(p) {
		if ok, err := path.Match(pattern, p); err == nil && ok {
			return true
		}
	}
	return false
}
