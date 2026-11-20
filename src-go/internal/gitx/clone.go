package gitx

import (
	"context"
	"fmt"
	"strings"
)

// Clone makes a full clone of url at dest: every branch and the whole history, never shallow.
func (g *Git) Clone(ctx context.Context, url, dest string) error {
	_, err := g.run(ctx, "", "clone", "--quiet", url, dest)
	return err
}

// FetchAll brings every branch and tag of origin into the clone at dir, dropping what origin no
// longer has.
func (g *Git) FetchAll(ctx context.Context, dir string) error {
	_, err := g.run(ctx, dir, "fetch", "--quiet", "--tags", "--prune", "origin")
	return err
}

// FastForwardToOriginHead moves the clone at dir's branch to origin's default branch as last
// fetched. A branch that cannot fast-forward (rewritten history, a local commit) is git's refusal,
// returned as an error.
func (g *Git) FastForwardToOriginHead(ctx context.Context, dir string) error {
	_, err := g.run(ctx, dir, "merge", "--quiet", "--ff-only", "refs/remotes/origin/HEAD")
	return err
}

// IsShallow reports whether the clone at dir holds only part of its history. Anything but git's
// own true or false is an error.
func (g *Git) IsShallow(ctx context.Context, dir string) (bool, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	switch reply := strings.TrimSpace(string(res.Stdout)); reply {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("git rev-parse --is-shallow-repository in %s gave %q, not true or false", dir, reply)
	}
}

// Unshallow fetches the rest of the history of a shallow clone at dir.
func (g *Git) Unshallow(ctx context.Context, dir string) error {
	_, err := g.run(ctx, dir, "fetch", "--quiet", "--unshallow", "origin")
	return err
}

// Head is the full sha of the commit checked out in the clone at dir.
func (g *Git) Head(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(res.Stdout))
	if !fullSHA(sha) {
		return "", fmt.Errorf("git rev-parse --verify HEAD in %s gave %q, not a full sha", dir, sha)
	}
	return sha, nil
}
