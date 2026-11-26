package gitx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The upstream questions (Plan 00265 Task 4.1, run.bash's checkout gate): what the work tree
// holds that no commit does, which branch is checked out, whether it tracks one on a remote,
// and how far apart the two are after a fetch. Each reads git's answer strictly: a git that
// cannot answer is an error, never "clean" or "level".

// symbolicRefDetached is git symbolic-ref --quiet's exit status for a HEAD that names a commit.
const symbolicRefDetached = 1

// statusRecordMin is the shortest porcelain v1 record: two status letters, a space, a path.
const statusRecordMin = 4

// leftRightCounts is the number of counts rev-list --left-right --count prints.
const leftRightCounts = 2

// ChangedPaths is every path git status reports in the work tree at dir: tracked changes,
// staged or not, and untracked files no ignore rule covers (an untracked directory as one
// path ending in a slash). A rename or copy is its new path.
func (g *Git) ChangedPaths(ctx context.Context, dir string) ([]string, error) {
	res, err := g.run(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(res.Stdout), "\x00")
	if text == "" {
		return nil, nil
	}
	records := strings.Split(text, "\x00")
	var out []string
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < statusRecordMin || rec[2] != ' ' {
			return nil, fmt.Errorf("git status printed a record it cannot read: %q", rec)
		}
		out = append(out, rec[3:])
		if rec[0] == 'R' || rec[0] == 'C' {
			if i+1 >= len(records) {
				return nil, fmt.Errorf("git status printed a rename with no source: %q", rec)
			}
			i++
		}
	}
	return out, nil
}

// Branch is the branch checked out at dir; onBranch is false for a detached HEAD.
func (g *Git) Branch(ctx context.Context, dir string) (branch string, onBranch bool, err error) {
	res, err := g.run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr.Code == symbolicRefDetached {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(res.Stdout)), true, nil
}

// HasUpstream reports whether branch at dir is set to track a branch (branch.<name>.merge).
func (g *Git) HasUpstream(ctx context.Context, dir, branch string) (bool, error) {
	if _, err := g.run(ctx, dir, "config", "--get", "branch."+branch+".merge"); err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr.Code == configNotSet {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// FetchUpstream fetches the remote the checked-out branch tracks (git's own choice for a bare
// `git fetch`), never asking for a user name or password on the terminal: a fetch that needs
// one fails instead of waiting.
func (g *Git) FetchUpstream(ctx context.Context, dir string) error {
	_, err := g.runner.Run(ctx, execx.Cmd{
		Name: gitProgram,
		Args: []string{"fetch", "--quiet"},
		Dir:  dir,
		Env:  []string{"GIT_TERMINAL_PROMPT=0"},
	})
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}

// Upstream is the short name of the branch the checked-out branch tracks (origin/F44). An
// upstream that is set but not on the remote is an error carrying git's words.
func (g *Git) Upstream(ctx context.Context, dir string) (string, error) {
	res, err := g.run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// AheadBehind is how many commits HEAD has that its upstream does not (ahead) and the
// upstream has that HEAD does not (behind), as of the last fetch.
func (g *Git) AheadBehind(ctx context.Context, dir string) (ahead, behind int, err error) {
	res, err := g.run(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0, err
	}
	bad := fmt.Errorf("git rev-list --count printed %q, not two counts", res.Stdout)
	fields := strings.Fields(string(res.Stdout))
	if len(fields) != leftRightCounts {
		return 0, 0, bad
	}
	ahead, aerr := strconv.Atoi(fields[0])
	behind, berr := strconv.Atoi(fields[1])
	if aerr != nil || berr != nil || ahead < 0 || behind < 0 {
		return 0, 0, bad
	}
	return ahead, behind, nil
}
