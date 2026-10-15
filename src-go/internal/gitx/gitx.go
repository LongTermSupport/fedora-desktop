// Package gitx answers the questions the tools ask git: is this directory a work tree, where
// is its top, what is origin, which files does it know, and what did a file hold at a ref.
// Each runs in a given directory through execx; it is the one package that runs git.
package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// Git runs git through a Runner.
type Git struct {
	runner execx.Runner
}

// New returns a Git over the given Runner.
func New(r execx.Runner) *Git {
	return &Git{runner: r}
}

// refused reports whether err is git running and exiting non-zero, as opposed to git
// being unable to start at all.
func refused(err error) bool {
	var exitErr *execx.ExitError
	return errors.As(err, &exitErr)
}

// ProjectHere reports whether dir is inside a git work tree. Git refusing (not a
// repository) is false; git being unrunnable is an error.
func (g *Git) ProjectHere(ctx context.Context, dir string) (bool, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if refused(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(res.Stdout)) == "true", nil
}

// TopLevel is the root of the work tree containing dir.
func (g *Git) TopLevel(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// RemoteURL is origin's URL, or "" when the repository has no origin. Reducing the URL to
// owner/repo is the caller's job.
func (g *Git) RemoteURL(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		if refused(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// ListFiles is the files git knows in the work tree at dir: tracked, plus untracked and not
// ignored, each once.
func (g *Git) ListFiles(ctx context.Context, dir string) ([]string, error) {
	res, err := g.run(ctx, dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
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

// FileAtRef is the text of path as committed at ref, and whether ref has it at all. A ref that
// does not resolve is an error: a typo must not read as "nothing to compare with".
func (g *Git) FileAtRef(ctx context.Context, dir, ref, path string) (string, bool, error) {
	if _, err := g.run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return "", false, fmt.Errorf("ref %q does not resolve to a commit: %w", ref, err)
	}
	listed, err := g.run(ctx, dir, "ls-tree", "--name-only", ref, "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(string(listed.Stdout)) == "" {
		return "", false, nil
	}
	shown, err := g.run(ctx, dir, "show", ref+":"+path)
	if err != nil {
		return "", false, err
	}
	return string(shown.Stdout), true, nil
}

// DefaultBranch is the branch the remote at url calls HEAD, read from `git ls-remote --symref`.
// A reply that names no branch is an error: nothing is guessed.
func (g *Git) DefaultBranch(ctx context.Context, url string) (string, error) {
	res, err := g.run(ctx, "", "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			if branch, _, found := strings.Cut(ref, "\t"); found && branch != "" {
				return branch, nil
			}
		}
	}
	return "", fmt.Errorf("%s names no default branch (git ls-remote --symref gave no symbolic HEAD)", url)
}

// RemoteBranchTip is the commit refs/heads/<branch> points at on the remote at url, read from
// `git ls-remote`. Anything but exactly one full sha is an error: nothing is guessed.
func (g *Git) RemoteBranchTip(ctx context.Context, url, branch string) (string, error) {
	ref := "refs/heads/" + branch
	res, err := g.run(ctx, "", "ls-remote", url, ref)
	if err != nil {
		return "", err
	}
	found := lines(res.Stdout)
	if len(found) == 1 {
		if sha, name, ok := strings.Cut(found[0], "\t"); ok && name == ref && fullSHA(sha) {
			return sha, nil
		}
	}
	return "", fmt.Errorf("%s gave no single commit for %s (git ls-remote printed %d line(s))", url, ref, len(found))
}

func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// CloneBranch clones one branch of url into dest.
func (g *Git) CloneBranch(ctx context.Context, url, branch, dest string) error {
	_, err := g.run(ctx, "", "clone", "--branch", branch, "--single-branch", url, dest)
	return err
}

// FetchBranch brings origin's branch in the clone at dir up to date as refs/remotes/origin/<branch>.
func (g *Git) FetchBranch(ctx context.Context, dir, branch string) error {
	_, err := g.run(ctx, dir, "fetch", "origin", "+refs/heads/"+branch+":"+RemoteRef(branch))
	return err
}

// RemoteHead is the commit refs/remotes/origin/<branch> points at in the clone at dir.
func (g *Git) RemoteHead(ctx context.Context, dir, branch string) (string, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--verify", RemoteRef(branch))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// RemoteRef is the local name FetchBranch gives origin's branch.
func RemoteRef(branch string) string { return "refs/remotes/origin/" + branch }

// IsAncestor reports whether commit ancestor is reachable from descendant (a commit is its own
// ancestor). Git answering "no" (exit 1) is false; any other refusal, such as an unknown
// object, or git being unable to start, is an error: nothing is guessed.
func (g *Git) IsAncestor(ctx context.Context, dir, ancestor, descendant string) (bool, error) {
	_, err := g.run(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr.Code == 1 {
		return false, nil
	}
	return false, err
}

// HasCommit reports whether the clone at dir holds the commit sha. Git refusing is false; git
// being unable to start is an error.
func (g *Git) HasCommit(ctx context.Context, dir, sha string) (bool, error) {
	if _, err := g.run(ctx, dir, "cat-file", "-e", sha+"^{commit}"); err != nil {
		if refused(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CommitsBetween is `<short sha> <subject>` for each commit in from..to, newest first.
func (g *Git) CommitsBetween(ctx context.Context, dir, from, to string) ([]string, error) {
	res, err := g.run(ctx, dir, "log", "--format=%h %s", from+".."+to)
	if err != nil {
		return nil, err
	}
	return lines(res.Stdout), nil
}

// ChangedPaths is the paths that differ between the commits from and to.
func (g *Git) ChangedPaths(ctx context.Context, dir, from, to string) ([]string, error) {
	res, err := g.run(ctx, dir, "diff", "--name-only", from, to)
	if err != nil {
		return nil, err
	}
	return lines(res.Stdout), nil
}

func lines(b []byte) []string {
	var out []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (g *Git) run(ctx context.Context, dir string, args ...string) (execx.Result, error) {
	res, err := g.runner.Run(ctx, execx.Cmd{Name: "git", Args: args, Dir: dir})
	if err != nil {
		return res, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}
