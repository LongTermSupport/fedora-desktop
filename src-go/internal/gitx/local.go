package gitx

import (
	"context"
	"strings"
)

// InitRepo makes dir a new repository whose first branch is branch.
func (g *Git) InitRepo(ctx context.Context, dir, branch string) error {
	_, err := g.run(ctx, dir, "init", "--quiet", "--initial-branch", branch)
	return err
}

// SetLocalConfig sets key in dir's own repository config, so nothing global is needed or changed.
func (g *Git) SetLocalConfig(ctx context.Context, dir, key, value string) error {
	_, err := g.run(ctx, dir, "config", "--local", key, value)
	return err
}

// Dirty reports whether dir's work tree has any change, untracked files included.
func (g *Git) Dirty(ctx context.Context, dir string) (bool, error) {
	res, err := g.run(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(res.Stdout)) != "", nil
}

// CommitAll stages every change in dir and commits it with message. The commit is never signed:
// a folder with no forge has no use for a signature, and a signing key that asks for a
// passphrase would stop the commit.
func (g *Git) CommitAll(ctx context.Context, dir, message string) error {
	if _, err := g.run(ctx, dir, "add", "--all"); err != nil {
		return err
	}
	_, err := g.run(ctx, dir, "-c", "commit.gpgsign=false", "commit", "--quiet", "--message", message)
	return err
}
