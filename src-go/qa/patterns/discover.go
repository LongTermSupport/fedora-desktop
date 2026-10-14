package patterns

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// script is one shell script to scan: its path in the repository and the name it has in the
// scan mirror.
type script struct {
	rel    string
	mirror string
}

// headBytes is how much of a file is read to see its shebang.
const headBytes = 1024

// linkCoverage is what became of the symlinks that are shell scripts: covered counts those whose
// target is itself scanned, and own lists "link -> target" for those scanned under the link's name
// because the target is not among the scripts.
type linkCoverage struct {
	covered int
	own     []string
}

// discover lists the shell scripts git would publish, outside the excluded trees, and what became
// of the symlinks that are scripts.
func (g *gate) discover(ctx context.Context) ([]script, linkCoverage, error) {
	var links linkCoverage
	trees, err := loadShellTrees(filepath.Join(g.scripts, treesFile))
	if err != nil {
		return nil, links, err
	}
	candidates, err := g.published(ctx)
	if err != nil {
		return nil, links, err
	}
	var scripts []script
	var symlinks []string
	for _, rel := range candidates {
		if excluded(rel, trees) {
			continue
		}
		k, classErr := g.classify(rel)
		if classErr != nil {
			return nil, links, classErr
		}
		switch k {
		case kindScript:
			scripts = append(scripts, script{rel: rel, mirror: mirrorName(rel)})
		case kindSymlinkedScript:
			symlinks = append(symlinks, rel)
		case kindOther:
		}
	}
	scripts, links, err = g.placeSymlinks(scripts, symlinks)
	if err != nil {
		return nil, links, err
	}
	if len(scripts) == 0 {
		return nil, links, fmt.Errorf("shell-script discovery found 0 shell scripts under %s: this repository always has some, so discovery is broken, and a pass here would vouch for code nothing had read", g.root)
	}
	return scripts, links, nil
}

// placeSymlinks leaves a link out of the scan only when its target is among the scripts, where it
// is scanned under its own name. Any other link (the target is ignored by git, in an excluded
// tree, or outside the root) is scanned under its own name, so no published path to shell goes
// unread.
func (g *gate) placeSymlinks(scripts []script, symlinks []string) ([]script, linkCoverage, error) {
	var links linkCoverage
	if len(symlinks) == 0 {
		return scripts, links, nil
	}
	scanned := make(map[string]bool, len(scripts))
	for _, s := range scripts {
		scanned[s.rel] = true
	}
	for _, rel := range symlinks {
		target, linkErr := filepath.EvalSymlinks(filepath.Join(g.root, filepath.FromSlash(rel)))
		if linkErr != nil {
			return nil, links, fmt.Errorf("the symlink %s cannot be followed: %w", rel, linkErr)
		}
		shown := target
		if inRoot, relErr := filepath.Rel(g.root, target); relErr == nil && inRoot != ".." && !strings.HasPrefix(inRoot, "../") {
			shown = filepath.ToSlash(inRoot)
			if scanned[shown] {
				links.covered++
				continue
			}
		}
		scripts = append(scripts, script{rel: rel, mirror: mirrorName(rel)})
		links.own = append(links.own, rel+" -> "+shown)
	}
	return scripts, links, nil
}

// published is the files git would publish (tracked, or untracked and not ignored) that are
// still in the work tree, sorted.
func (g *gate) published(ctx context.Context) ([]string, error) {
	listed, err := g.gitList(ctx, "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	deleted, err := g.gitList(ctx, "--deleted")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, rel := range listed {
		if !slices.Contains(deleted, rel) {
			files = append(files, rel)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

func (g *gate) gitList(ctx context.Context, flags ...string) ([]string, error) {
	argv := append([]string{"git", "ls-files", "-z"}, flags...)
	res, err := g.runTool(ctx, gitTimeout, g.root, argv...)
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under %s: %w", g.root, err)
	}
	var files []string
	for p := range strings.SplitSeq(res.Stdout, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// loadShellTrees reads the directory names the shell gates must not gate on (the shell rows of
// qa-trees.tsv). Finding none is an error: an empty list would scan trees that are not ours.
func loadShellTrees(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the excluded trees: %w", err)
	}
	trees, err := parseTrees(f, file)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("%s: %w", file, closeErr)
	}
	return trees, err
}

func parseTrees(r io.Reader, name string) ([]string, error) {
	var trees []string
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("%s:%d: expected '<list><TAB><dir>', got %q", name, n, line)
		}
		if fields[0] == "shell" {
			trees = append(trees, fields[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(trees) == 0 {
		return nil, fmt.Errorf("%s has no 'shell' row: with no excluded trees the gate would scan trees that are not ours", name)
	}
	return trees, nil
}

// excluded is whether the path lies inside an excluded tree: /dir/ occurs as whole segments
// anywhere in /path/.
func excluded(rel string, trees []string) bool {
	within := "/" + rel + "/"
	return slices.ContainsFunc(trees, func(dir string) bool { return strings.Contains(within, "/"+dir+"/") })
}

// kind is what a published path is to this gate.
type kind int

const (
	kindOther kind = iota
	kindScript
	// kindSymlinkedScript is a symlink to a shell script: the target is scanned under its own name.
	kindSymlinkedScript
)

func (g *gate) classify(rel string) (kind, error) {
	full := filepath.Join(g.root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return kindOther, fmt.Errorf("cannot inspect %s: %w", rel, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return classifyLink(rel, full)
	}
	if !info.Mode().IsRegular() {
		return kindOther, nil
	}
	isScript, err := isShellScript(full, rel)
	if err != nil || !isScript {
		return kindOther, err
	}
	return kindScript, nil
}

func classifyLink(rel, full string) (kind, error) {
	target, err := os.Stat(full)
	if err != nil {
		if hasShellExt(rel) {
			return kindOther, fmt.Errorf("the symlink %s cannot be followed: %w", rel, err)
		}
		return kindOther, nil
	}
	if !target.Mode().IsRegular() {
		return kindOther, nil
	}
	isScript, err := isShellScript(full, rel)
	if err != nil || !isScript {
		return kindOther, err
	}
	return kindSymlinkedScript, nil
}

func hasShellExt(rel string) bool {
	ext := path.Ext(rel)
	return ext == ".sh" || ext == ".bash"
}

// isShellScript decides by what the file is: a .sh/.bash name, or a shell shebang. A template
// (.j2) is not shell until rendered.
func isShellScript(full, rel string) (bool, error) {
	switch {
	case path.Ext(rel) == ".j2":
		return false, nil
	case hasShellExt(rel):
		return true, nil
	}
	head, err := readHead(full)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	if i := strings.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	return discovery.ShellShebang(head), nil
}

func readHead(full string) (string, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	head, err := io.ReadAll(io.LimitReader(f, headBytes))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return string(head), err
}

// mirrorName is the name a script has in the scan mirror: semgrep picks the language from the
// extension, so a script without a shell extension gets .bash.
func mirrorName(rel string) string {
	if hasShellExt(rel) {
		return rel
	}
	return rel + ".bash"
}

// buildMirror copies the scripts into dir at their mirror names, line endings normalised to LF. Two scripts claiming one name
// would silently replace each other, so that is an error.
func (g *gate) buildMirror(dir string, scripts []script) error {
	// semgrep drops tests/, vendor/, build/ and more by default; an empty ignore file keeps every
	// script the gate discovered.
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cannot build the scan mirror: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".semgrepignore"), nil, 0o600); err != nil {
		return fmt.Errorf("cannot build the scan mirror: %w", err)
	}
	claimed := make(map[string]string, len(scripts))
	for _, s := range scripts {
		if other, taken := claimed[s.mirror]; taken {
			return fmt.Errorf("two files claim the same scan-mirror path '%s':\n    %s\n    %s\n  One would silently replace the other and vanish from this gate entirely", s.mirror, other, s.rel)
		}
		claimed[s.mirror] = s.rel
		data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(s.rel)))
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", s.rel, err)
		}
		dest := filepath.Join(dir, filepath.FromSlash(s.mirror))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return fmt.Errorf("cannot build the scan mirror: %w", err)
		}
		if err := os.WriteFile(dest, bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), 0o600); err != nil {
			return fmt.Errorf("cannot build the scan mirror: %w", err)
		}
	}
	return nil
}
