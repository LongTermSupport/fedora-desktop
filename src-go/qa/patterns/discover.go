package patterns

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// script is one shell script to scan: its path in the repository and the name it has in the
// scan mirror.
type script struct {
	rel    string
	mirror string
}

// headBytes is how much of a file is read to see its shebang.
const headBytes = 1024

// discover lists the shell scripts git would publish, outside the excluded trees, and the
// count of symlinks that are scripts but are not scanned under their own name.
func (g *gate) discover(ctx context.Context) ([]script, int, error) {
	trees, err := loadShellTrees(filepath.Join(g.scripts, treesFile))
	if err != nil {
		return nil, 0, err
	}
	candidates, err := g.published(ctx)
	if err != nil {
		return nil, 0, err
	}
	var scripts []script
	symlinks := 0
	for _, rel := range candidates {
		if excluded(rel, trees) {
			continue
		}
		k, err := g.classify(rel)
		if err != nil {
			return nil, 0, err
		}
		switch k {
		case kindScript:
			scripts = append(scripts, script{rel: rel, mirror: mirrorName(rel)})
		case kindSymlinkedScript:
			symlinks++
		case kindOther:
		}
	}
	if len(scripts) == 0 {
		return nil, 0, fmt.Errorf("shell-script discovery found 0 shell scripts under %s: this repository always has some, so discovery is broken, and a pass here would vouch for code nothing had read", g.root)
	}
	return scripts, symlinks, nil
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
	return shellShebang(head), nil
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

// shellShebang is a first line that runs bash or sh as a script interpreter, directly or
// through env, without -c (an inline one-liner in another kind of file, such as a playbook).
func shellShebang(line string) bool {
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return false
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 && path.Base(fields[0]) == "env" {
		fields = fields[1:]
		for len(fields) > 0 && (strings.HasPrefix(fields[0], "-") || strings.Contains(fields[0], "=")) {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return false
	}
	if interp := path.Base(fields[0]); interp != "bash" && interp != "sh" {
		return false
	}
	return !slices.Contains(fields[1:], "-c")
}

// mirrorName is the name a script has in the scan mirror: semgrep picks the language from the
// extension, so a script without a shell extension gets .bash.
func mirrorName(rel string) string {
	if hasShellExt(rel) {
		return rel
	}
	return rel + ".bash"
}

// buildMirror copies the scripts into dir at their mirror names. Two scripts claiming one name
// would silently replace each other, so that is an error.
func (g *gate) buildMirror(dir string, scripts []script) error {
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
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return fmt.Errorf("cannot build the scan mirror: %w", err)
		}
	}
	return nil
}
