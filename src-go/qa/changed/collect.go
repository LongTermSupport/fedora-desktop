package changed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// collected is the changed files: those still on disk, those deleted (which have nothing left
// to check but may still owe a whole gate), and one note per git-listed path that is neither.
type collected struct {
	existing, deleted, notes []string
}

// git runs git in the root and returns its standard output, failing on any non-zero status.
func (s *session) git(ctx context.Context, args ...string) (string, error) {
	res, err := s.runCmd(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: s.root})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// nulList splits git's -z output.
func nulList(out string) []string {
	var names []string
	for name := range strings.SplitSeq(out, "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// collectGit lists the files changed against base (the working tree against base, so staged,
// unstaged and committed-since-base changes all appear) plus untracked files that are not
// ignored. A failing git is an error: a bad ref must not read as "nothing to check".
func (s *session) collectGit(ctx context.Context, base string) (collected, error) {
	fail := func(err error) (collected, error) {
		return collected{}, fmt.Errorf("cannot list changed files vs %s: %w", base, err)
	}
	present, err := s.git(ctx, "diff", "--name-only", "-z", "--diff-filter=d", base, "--")
	if err != nil {
		return fail(err)
	}
	removed, err := s.git(ctx, "diff", "--name-only", "-z", "--diff-filter=D", base, "--")
	if err != nil {
		return fail(err)
	}
	untracked, err := s.git(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return fail(err)
	}
	names := nulList(present + "\x00" + untracked)
	slices.Sort(names)
	var out collected
	for _, name := range slices.Compact(names) {
		regular, err := isRegular(filepath.Join(s.root, name))
		if err != nil {
			return collected{}, err
		}
		if regular {
			out.existing = append(out.existing, name)
		} else {
			out.notes = append(out.notes, name+": not a regular file (a directory, submodule or dangling link), not checked")
		}
	}
	gone := nulList(removed)
	slices.Sort(gone)
	for _, name := range slices.Compact(gone) {
		if !slices.Contains(out.existing, name) {
			out.deleted = append(out.deleted, name)
		}
	}
	return out, nil
}

// isRegular is whether path is a regular file, following symlinks; a path that is not there is
// simply not a file.
func isRegular(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	return info.Mode().IsRegular(), nil
}

// collectExplicit takes the files named on the command line: cleaned to repo-relative paths
// (a path outside the repository is refused), de-duplicated, and a path that is not there is
// dropped with a word on stderr. A named directory or other non-file is refused.
func (s *session) collectExplicit(args []string) (collected, error) {
	var out collected
	for _, arg := range args {
		rel, err := s.relative(arg)
		if err != nil {
			return collected{}, err
		}
		if slices.Contains(out.existing, rel) {
			continue
		}
		info, err := os.Stat(filepath.Join(s.root, rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			s.errf("qa-changed: %s does not exist, ignored\n", rel)
		case err != nil:
			return collected{}, fmt.Errorf("cannot stat %s: %w", rel, err)
		case !info.Mode().IsRegular():
			return collected{}, fmt.Errorf("%s is not a regular file", rel)
		default:
			out.existing = append(out.existing, rel)
		}
	}
	return out, nil
}

// relative cleans a path given on the command line to a path relative to the root.
func (s *session) relative(arg string) (string, error) {
	rel := filepath.Clean(arg)
	if filepath.IsAbs(rel) {
		var err error
		if rel, err = filepath.Rel(s.root, rel); err != nil {
			return "", fmt.Errorf("%s is outside the repository %s", arg, s.root)
		}
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the repository %s or is the root itself", arg, s.root)
	}
	return rel, nil
}
