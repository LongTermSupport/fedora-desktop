package python

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/published"
)

// sources is what the gate checks: the Python files and the Python templates, as paths relative
// to the root, sorted.
type sources struct {
	files     []string
	templates []string
}

// kind is what a published path is to this gate.
type kind int

const (
	kindOther kind = iota
	kindFile
	kindTemplate
)

// discover lists what git would publish, outside the excluded trees, that is Python by what it
// is: a .py name or a Python shebang, whatever the mode.
func (g *gate) discover(ctx context.Context) (sources, error) {
	var out sources
	trees, err := published.Trees(filepath.Join(g.scripts, treesFile), "python")
	if err != nil {
		return out, err
	}
	candidates, err := published.Files(ctx, g.env.Runner, g.root)
	if err != nil {
		return out, err
	}
	for _, rel := range candidates {
		if published.Excluded(rel, trees) {
			continue
		}
		k, classErr := g.classify(rel)
		if classErr != nil {
			return out, classErr
		}
		switch k {
		case kindFile:
			out.files = append(out.files, rel)
		case kindTemplate:
			out.templates = append(out.templates, rel)
		case kindOther:
		}
	}
	if len(out.files) == 0 {
		return out, fmt.Errorf("file discovery found 0 Python files under %s: this repository always has some, so discovery is broken, and a pass here would vouch for code nothing had read", g.root)
	}
	return out, nil
}

// classify judges one published path by what it is; a symlink by its target.
func (g *gate) classify(rel string) (kind, error) {
	full := filepath.Join(g.root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return kindOther, fmt.Errorf("cannot inspect %s: %w", rel, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if info, err = os.Stat(full); err != nil {
			if path.Ext(rel) == ".py" {
				return kindOther, fmt.Errorf("the symlink %s cannot be followed: %w", rel, err)
			}
			// Nothing is reachable through it, so it is no program of ours.
			return kindOther, nil
		}
	}
	if !info.Mode().IsRegular() {
		return kindOther, nil
	}
	ext := path.Ext(rel)
	if ext == ".py" {
		return kindFile, nil
	}
	first, err := published.FirstLine(full)
	if err != nil {
		return kindOther, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	if !pythonShebang(first) {
		return kindOther, nil
	}
	if ext == ".j2" {
		return kindTemplate, nil
	}
	return kindFile, nil
}

// pythonShebang is a first line that begins #! and names python anywhere after it.
func pythonShebang(line string) bool {
	rest, ok := strings.CutPrefix(line, "#!")
	return ok && strings.Contains(rest, "python")
}
