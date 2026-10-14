package bashlimits

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// Git runs git in a directory and returns its standard output. This module is the nested QA
// module and cannot import the main module's execx and gitx (go.mod there must stay free of
// dependencies, and a local replace here is refused by the linter), so git is run here, in one
// place, behind this interface.
type Git interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// RefusedError is git running and exiting non-zero, as against git being unable to start.
type RefusedError struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), strings.TrimSpace(e.Stderr))
}

// OSGit runs the git on PATH through the shared process seam.
type OSGit struct{}

// Run implements Git.
func (OSGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	res, err := proc.OS{}.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: dir})
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if res.Code != 0 {
		return "", &RefusedError{Args: args, Code: res.Code, Stderr: res.Stderr}
	}
	return res.Stdout, nil
}

// gitRepo answers the questions the gate asks git about one working directory.
type gitRepo struct {
	git Git
	dir string
}

// topLevel is the root of the work tree containing dir.
func (r gitRepo) topLevel(ctx context.Context) (string, error) {
	out, err := r.git.Run(ctx, r.dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// listFiles is the files git knows in the work tree: tracked, plus untracked and not ignored,
// each once.
func (r gitRepo) listFiles(ctx context.Context) ([]string, error) {
	out, err := r.git.Run(ctx, r.dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	seen := map[string]bool{}
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	return files, nil
}

// fileAtRef is the text of path as committed at ref, and whether ref has it at all. A ref that
// does not resolve is an error: a typo must not read as "nothing to compare with".
func (r gitRepo) fileAtRef(ctx context.Context, ref, path string) (string, bool, error) {
	if _, err := r.git.Run(ctx, r.dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return "", false, fmt.Errorf("ref %q does not resolve to a commit: %w", ref, err)
	}
	listed, err := r.git.Run(ctx, r.dir, "ls-tree", "--name-only", ref, "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(listed) == "" {
		return "", false, nil
	}
	text, err := r.git.Run(ctx, r.dir, "show", ref+":"+path)
	return text, err == nil, err
}

// mergeBaseWithDefault is the commit HEAD shares with origin's default branch: the point a
// change started from, however many commits it has made and whether or not they are pushed.
// A repository whose default branch is unknown is an error, not "compare with HEAD".
func (r gitRepo) mergeBaseWithDefault(ctx context.Context) (string, error) {
	head, err := r.git.Run(ctx, r.dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if refused, isRefusal := errors.AsType[*RefusedError](err); isRefusal {
		return "", fmt.Errorf("origin's default branch is not known here (refs/remotes/origin/HEAD is unset, git exit %d; run: git remote set-head origin --auto): %w", refused.Code, err)
	}
	if err != nil {
		return "", err
	}
	base, err := r.git.Run(ctx, r.dir, "merge-base", "HEAD", strings.TrimSpace(head))
	return strings.TrimSpace(base), err
}
