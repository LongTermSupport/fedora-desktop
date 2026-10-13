// Package gitx answers the few questions ccy-teams asks git: is this directory a work tree,
// where is its top, and what is origin. Each runs in a given directory through execx.
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

func (g *Git) run(ctx context.Context, dir string, args ...string) (execx.Result, error) {
	res, err := g.runner.Run(ctx, execx.Cmd{Name: "git", Args: args, Dir: dir})
	if err != nil {
		return res, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}
