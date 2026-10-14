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

func (g *Git) run(ctx context.Context, dir string, args ...string) (execx.Result, error) {
	res, err := g.runner.Run(ctx, execx.Cmd{Name: "git", Args: args, Dir: dir})
	if err != nil {
		return res, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}
