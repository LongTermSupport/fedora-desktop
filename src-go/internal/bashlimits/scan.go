package bashlimits

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ListFiles is the files git knows in the work tree at root: tracked, plus untracked and not
// ignored, so a script is held to the limits before it is added.
func ListFiles(ctx context.Context, r execx.Runner, root string) ([]string, error) {
	res, err := r.Run(ctx, execx.Cmd{
		Name: "git",
		Args: []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard"},
		Dir:  root,
	})
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	var out []string
	seen := map[string]bool{}
	for p := range strings.SplitSeq(string(res.Stdout), "\x00") {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// Scan is the result of measuring a tree.
type Scan struct {
	Measured map[string]Measurement // every non-exempt bash file
	Exempt   int                    // bash files an Exemption covers
}

// ScanTree measures each bash file among files. A listed file that is gone from disk, or is not
// a regular file (a symlink, a submodule), is not scanned; any other failure stops the scan.
func ScanTree(fsys fs.FS, files []string) (Scan, error) {
	scan := Scan{Measured: map[string]Measurement{}}
	for _, p := range files {
		info, err := fs.Lstat(fsys, p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Scan{}, fmt.Errorf("stat %s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := scanFile(fsys, p, &scan); err != nil {
			return Scan{}, err
		}
	}
	return scan, nil
}

func scanFile(fsys fs.FS, p string, scan *Scan) error {
	bash, err := IsCandidate(fsys, p)
	if err != nil || !bash {
		return err
	}
	_, exempt, err := ExemptionFor(p)
	if err != nil {
		return err
	}
	if exempt {
		scan.Exempt++
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	scan.Measured[p] = Measure(src)
	return nil
}

// FileAtRef is the text of path as committed at ref, and whether ref has it at all. A ref that
// does not resolve is an error: a typo must not read as "nothing to compare with".
func FileAtRef(ctx context.Context, r execx.Runner, root, ref, path string) (string, bool, error) {
	if _, err := git(ctx, r, root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return "", false, fmt.Errorf("base ref %q does not resolve to a commit: %w", ref, err)
	}
	listed, err := git(ctx, r, root, "ls-tree", "--name-only", ref, "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(listed) == "" {
		return "", false, nil
	}
	text, err := git(ctx, r, root, "show", ref+":"+path)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func git(ctx context.Context, r execx.Runner, root string, args ...string) (string, error) {
	res, err := r.Run(ctx, execx.Cmd{Name: "git", Args: args, Dir: root})
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(res.Stdout), nil
}
